import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'data/season.dart';
import 'data/session.dart';
import 'data/cart.dart';
import 'data/app_info.dart';
import 'data/wishlist.dart';
import 'data/urls.dart';
import 'data/staff.dart';
import 'widgets/app_shell.dart';
import 'widgets/design_system.dart';
import 'widgets/app_nav.dart';
import 'screens/admin_screen.dart';
import 'screens/cart_screen.dart';
import 'screens/profile_screen.dart';
import 'screens/seller_dashboard_screen.dart';
import 'screens/wishlist_screen.dart';
import 'screens/delivery_screen.dart';
import 'screens/home_screen.dart';
import 'screens/login_screen.dart';

// ponytail: iOS 26 simulator breaks Dart TLS verify (CERTIFICATE_VERIFY_FAILED,
// simulator-only). Debug-only bypass; remove when the Flutter engine fix ships.
class _DevHttpOverrides extends HttpOverrides {
  @override
  HttpClient createHttpClient(SecurityContext? context) =>
      super.createHttpClient(context)
        ..badCertificateCallback = (cert, host, port) => true;
}

void main() async {
  if (kDebugMode) HttpOverrides.global = _DevHttpOverrides();
  // Real paths rather than /#/: the staff panels are URLs people type, and
  // Vercel already rewrites everything to index.html.
  useCleanUrls();
  // The stored session decides whether the login screen shows at all, so it
  // has to be read before the first frame.
  WidgetsFlutterBinding.ensureInitialized();
  // Keep web semantics available without requiring a hidden opt-in gesture.
  if (kIsWeb) WidgetsBinding.instance.ensureSemantics();
  await AppInfo.load();
  await Cart.instance.restore();
  await Wishlist.instance.restore();
  await Session.instance.restore();
  await StaffSession.admin.restore();
  await StaffSession.rider.restore();
  // Decides what colour the shop is, so it is read before the first frame
  // rather than repainting the header a second after it draws. It swallows
  // its own failures — a festival must never be the reason the shop will not
  // open — so there is nothing to catch here.
  await Seasons.instance.load();
  runApp(const LamazonApp());
}

/// Resolves the addresses the app knows by name — the two staff panels and
/// the bottom bar's destinations — or null for everything else, which is what
/// sends a shopper to the shop they were asking for.
Route<dynamic>? _staffRoute(RouteSettings settings) {
  final path = (settings.name ?? '/').toLowerCase().replaceAll(
    RegExp(r'/+$'),
    '',
  );
  final screen = switch (path) {
    '/admin' || '/admin/log_in' => const AdminScreen(),
    '/delivery' => const DeliveryScreen(),
    // The bottom bar's destinations. Built here because nothing imports
    // main.dart, so naming them costs no import cycle — the nav asks for a
    // path and this is the only place that knows what a path is made of.
    AppRoutes.cart => const CartScreen(),
    AppRoutes.login => const LoginScreen(),
    AppRoutes.saved => const WishlistScreen(),
    AppRoutes.store => const SellerDashboardScreen(),
    AppRoutes.account => const ProfileScreen(),
    _ => null,
  };
  if (screen == null) return null;
  return MaterialPageRoute<void>(builder: (_) => screen, settings: settings);
}

Route<dynamic> _shopRoute() => MaterialPageRoute<void>(
  settings: const RouteSettings(name: '/'),
  builder: (_) =>
      Session.instance.onboarded ? const HomeScreen() : const LoginScreen(),
);

class LamazonApp extends StatelessWidget {
  const LamazonApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'Unimiunte',
      debugShowCheckedModeBanner: false,
      theme: LamazonTheme.data,
      // Shared responsive canvas; individual screens choose their reading width.
      builder: (context, child) => AppShell(child: child!),
      // The staff panels are their own entrances, on purpose: nothing in the
      // shopper's app links to them, and neither one uses a shopper session.
      //
      // These are addresses people type, so they are matched forgivingly —
      // case and a trailing slash do not decide whether the panel opens.
      // Chrome autocompleting "/admin/Log_IN" missed a route table spelled
      // "/admin/log_IN" and dropped the admin on the shop's front page with
      // no explanation.
      // Every route is built here rather than through `home`, which cannot
      // coexist with onGenerateInitialRoutes — and an app whose first screen
      // depends on the address needs both to agree.
      onGenerateRoute: (settings) => _staffRoute(settings) ?? _shopRoute(),
      // A deep link would otherwise be built one segment at a time, leaving
      // /admin under /admin/log_IN in the stack. One address, one screen.
      onGenerateInitialRoutes: (initial) => [
        _staffRoute(RouteSettings(name: initial)) ?? _shopRoute(),
      ],
    );
  }
}
