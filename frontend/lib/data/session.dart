import 'package:flutter/foundation.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'addresses.dart';
import 'api.dart';
import 'orders.dart';
import 'seller.dart';

/// Sign-in state from the emailed-code flow, persisted so a browser refresh
/// does not sign the seller out.
///
/// Two tokens: a short-lived access token that rides on every request, and a
/// long-lived refresh token that buys a new pair. [freshToken] is all callers
/// need — it renews in the background before the access token dies.
class Session extends ChangeNotifier {
  Session._();
  static final Session instance = Session._();

  static const _emailKey = 'session.email';
  static const _tokenKey = 'session.token';
  static const _refreshKey = 'session.refresh';
  static const _expiryKey = 'session.expiresAt';

  String? _email;
  String? _token;
  Map<String, dynamic>? _user; // id, name, phone, roles — from the server
  String? _refreshToken;
  DateTime? _expiresAt;
  bool _skipped = false;

  String? get email => _email;
  String? get token => _token;
  bool get loggedIn => _email != null;

  /// The public id a person quotes at support, e.g. LMZ-1042.
  String get publicId => _user?['id'] as String? ?? '';
  String get name => _user?['name'] as String? ?? '';
  String get phone => _user?['phone'] as String? ?? '';

  /// "buyer", or "buyer" and "seller" once they open a store. Derived on the
  /// server from whether a store exists, so it is never stale.
  List<String> get roles =>
      (_user?['roles'] as List<dynamic>? ?? const ['buyer']).cast<String>();
  bool get isSeller => roles.contains('seller');

  /// Whether we have what an order needs: a name, a number, and somewhere to
  /// deliver. The server decides — it can see the address book — and false
  /// sends a new arrival to the details screen instead of into a shop that
  /// cannot deliver to them.
  bool get ready => _user?['ready'] == true;
  bool get hasPassword => _user?['hasPassword'] == true;

  /// True once the user has either logged in or chosen to browse as a guest,
  /// so the login screen is not shown again.
  bool get onboarded => loggedIn || _skipped;

  /// Anything with a name, an @ and a dotted domain after it.
  static bool isValidEmail(String value) =>
      RegExp(r'^[\w.+-]+@[\w-]+\.[\w.-]+$').hasMatch(value.trim());

  /// Reads the stored session at startup. Called before the first frame.
  Future<void> restore() async {
    final prefs = await SharedPreferences.getInstance();
    _email = prefs.getString(_emailKey);
    _token = prefs.getString(_tokenKey);
    _refreshToken = prefs.getString(_refreshKey);
    final expiry = prefs.getString(_expiryKey);
    _expiresAt = expiry == null ? null : DateTime.tryParse(expiry);
    if (_email == null) {
      // Nobody to fetch a book for; let anything waiting on it carry on.
      AddressBook.instance.markLoaded();
    } else {
      notifyListeners();
      // The profile and address book live on the server, so a restored
      // session fetches them rather than trusting anything cached here.
      refreshProfile();
    }
  }

  /// Reads the profile and address book back from the server. Safe to call
  /// whenever; failures leave the last known values alone.
  Future<void> refreshProfile() async {
    if (_token == null) return;
    try {
      _user = await Api.instance.me();
      notifyListeners();
    } catch (e) {
      logApiFailure('profile', e);
    }
    await AddressBook.instance.load();
    // The store lives on the server, so a fresh sign-in has to fetch it.
    // Without this the app forgets someone is a seller the moment they log
    // out, and offers them "Sell on Unimiunte" for a store they already have.
    if (isSeller) await Seller.instance.load();
  }

  Future<void> signIn(AuthTokens tokens) async {
    _email = tokens.email;
    _token = tokens.token;
    _refreshToken = tokens.refreshToken;
    // Renew a minute early, so a request never leaves with a token that
    // expires while it is in flight.
    _expiresAt = DateTime.now()
        .add(Duration(seconds: tokens.expiresIn))
        .subtract(const Duration(minutes: 1));
    _skipped = false;
    await _save();
    notifyListeners();
    await refreshProfile();
  }

  /// A usable access token, renewed first if it is close to expiring. Null
  /// when signed out or when the refresh token is spent — the caller carries
  /// on as a guest and the login screen asks for a new code.
  Future<String?> freshToken() async {
    if (_token == null || _refreshToken == null) return _token;
    if (_expiresAt != null && DateTime.now().isBefore(_expiresAt!)) {
      return _token;
    }
    try {
      await signIn(await Api.instance.refreshSession(_refreshToken!));
      return _token;
    } catch (e) {
      logApiFailure('session refresh', e);
      await signOut();
      return null;
    }
  }

  /// Guest browsing. Nothing is stored, because there is nothing to remember.
  void skip() {
    AddressBook.instance.markLoaded();
    _skipped = true;
    notifyListeners();
  }

  Future<void> signOut() async {
    _email = null;
    _token = null;
    _user = null;
    AddressBook.instance.clear();
    // Orders belong to the person who placed them, not to the browser.
    MyOrders.instance.clear();
    _refreshToken = null;
    _expiresAt = null;
    _skipped = true;
    final prefs = await SharedPreferences.getInstance();
    for (final key in [_emailKey, _tokenKey, _refreshKey, _expiryKey]) {
      await prefs.remove(key);
    }
    notifyListeners();
  }

  Future<void> _save() async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_emailKey, _email!);
    await prefs.setString(_tokenKey, _token!);
    await prefs.setString(_refreshKey, _refreshToken!);
    await prefs.setString(_expiryKey, _expiresAt!.toIso8601String());
  }
}
