import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;

import '../data/api.dart';
import 'package:lucide_icons_flutter/lucide_icons.dart';

import '../data/catalog.dart';
import '../data/session.dart';
import '../widgets/app_shell.dart';
import '../widgets/design_system.dart';
import '../widgets/image_marquee.dart';
import 'home_screen.dart';
import 'policy_screen.dart';
import 'profile_setup_screen.dart';

/// Opening screen: drifting product tiles, the Unimiunte mark, and an email
/// sign-in that can be skipped.
class LoginScreen extends StatefulWidget {
  const LoginScreen({super.key});

  @override
  State<LoginScreen> createState() => _LoginScreenState();
}

/// What the sign-in card is asking for right now.
enum _Step { email, code, password }

class _LoginScreenState extends State<LoginScreen> {
  final _email = TextEditingController();
  final _code = TextEditingController();
  final _password = TextEditingController();

  /// What the card is asking for. The address decides: one that has a
  /// password is asked for it, everyone else gets a code in the post.
  _Step _step = _Step.email;
  bool _busy = false;
  String? _error;

  /// Whether the terms and privacy policies are actually written. Consent
  /// against a page that reads "This policy is not published yet" is consent
  /// against nothing, so the line below the button waits for a real document
  /// rather than pointing at an empty one.
  bool _policiesPublished = false;

  @override
  void initState() {
    super.initState();
    _checkPolicies();
  }

  Future<void> _checkPolicies() async {
    try {
      final docs = await loadPolicies();
      final needed = {'terms', 'privacy'};
      final live = docs
          .where((d) => needed.contains(d.slug) && d.published)
          .length;
      if (mounted) setState(() => _policiesPublished = live == needed.length);
    } catch (e) {
      // Offline is not proof they are published, so the line stays off.
      logApiFailure('policies', e);
    }
  }

  @override
  void dispose() {
    _email.dispose();
    _code.dispose();
    _password.dispose();
    super.dispose();
  }

  /// The controller for the step on screen. One getter, because three places
  /// used to switch on _step to find it and a fourth switched on it to decide
  /// what had been typed.
  TextEditingController get _typedInto => switch (_step) {
    _Step.email => _email,
    _Step.code => _code,
    _Step.password => _password,
  };

  bool get _valid => switch (_step) {
    _Step.email => Session.isValidEmail(_email.text),
    _Step.code => RegExp(r'^\d{6}$').hasMatch(_code.text.trim()),
    _Step.password => _password.text.isNotEmpty,
  };

  /// Step one: find out what this address is signed in with.
  Future<void> _start() async {
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final next = await Api.instance.startLogin(_email.text.trim());
      // The server can sign someone in on the spot when it is running with
      // the code switched off; asking for one that was never sent would be a
      // dead end.
      if (next.tokens != null) {
        await Session.instance.signIn(next.tokens!);
        _go();
        return;
      }
      setState(() => _step = next.needsPassword ? _Step.password : _Step.code);
    } on http.ClientException catch (e) {
      setState(() => _error = e.message);
    } catch (e) {
      // Say so. This used to wave the person through as a guest, on the
      // theory that browsing beats a dead end — but from where they sit,
      // asking for a code and being handed the shop with no code and no
      // message is indistinguishable from the button not working.
      logApiFailure('login code', e);
      setState(
        () => _error =
            'Could not reach the server, so no code went out. Try again, or '
            'use Browse the shop to look around.',
      );
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  /// Step two: the code buys a session token.
  Future<void> _verifyCode() async {
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final tokens = await Api.instance.verifyLoginCode(
        _email.text.trim(),
        _code.text.trim(),
      );
      await Session.instance.signIn(tokens);
      _go();
    } on http.ClientException catch (e) {
      setState(() => _error = e.message);
    } catch (e) {
      logApiFailure('verify code', e);
      setState(() => _error = 'Could not reach the server. Try again.');
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  /// The password path, for an address that has one.
  Future<void> _verifyPassword() async {
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final tokens = await Api.instance.passwordLogin(
        _email.text.trim(),
        _password.text,
      );
      await Session.instance.signIn(tokens);
      _go();
    } on http.ClientException catch (e) {
      setState(() => _error = e.message);
    } catch (e) {
      logApiFailure('password login', e);
      setState(() => _error = 'Could not reach the server. Try again.');
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  /// One button, whichever step it is on.
  void _submit() => switch (_step) {
    _Step.email => _start(),
    _Step.code => _verifyCode(),
    _Step.password => _verifyPassword(),
  };

  void _enter({required bool skip}) {
    if (skip) Session.instance.skip();
    _go();
  }

  /// Leaves the login screen for wherever the user came from — via the
  /// details form when this is somebody's first time, since an order needs a
  /// name, a number and an address and now is the one moment they will fill
  /// them in.
  Future<void> _go() async {
    if (!mounted) return;
    if (Session.instance.loggedIn && !Session.instance.ready) {
      await Navigator.push(
        context,
        MaterialPageRoute(builder: (_) => const ProfileSetupScreen()),
      );
      if (!mounted) return;
    }
    // Opened from the account screen: go back to it, now signed in. At app
    // launch there is nothing to go back to, so home takes over instead.
    if (Navigator.canPop(context)) {
      Navigator.pop(context);
      return;
    }
    Navigator.pushReplacement(
      context,
      MaterialPageRoute(builder: (_) => const HomeScreen()),
    );
  }

  /// What the person has typed into the field this step is asking about.
  String get _typed => _typedInto.text.trim();

  @override
  Widget build(BuildContext context) {
    // What the shop actually sells, drifting past behind the sign-in card.
    //
    // shownCatalog, not the bundled `products` list: this used to advertise
    // about a dozen sample photographs — bread, pizza, a teddy bear — that
    // are not in the catalogue and cannot be bought. A first impression
    // built out of stock that does not exist is the worst possible one for
    // a shop to make.
    final urls = [
      for (final tab in ['Electronics', 'Grocery', 'Food', 'Gifts', 'Beauty'])
        // Thumbnails: the backdrop tiles are ~104px, so full photos would
        // burn megabytes on first paint for no visible gain.
        ...shownCatalog
            .where((p) => p.tab == tab && p.imageUrl.trim().isNotEmpty)
            .map((p) => thumb(p.imageUrl, 200)),
    ];
    return Scaffold(
      backgroundColor: LamazonTheme.canvas,
      body: Stack(
        children: [
          Positioned(
            top: 0,
            left: 0,
            right: 0,
            child: SafeArea(
              bottom: false,
              // Fades the drifting tiles into the background so the logo and
              // sign-in card sit on clean space.
              child: ShaderMask(
                shaderCallback: (rect) => const LinearGradient(
                  begin: Alignment.topCenter,
                  end: Alignment.bottomCenter,
                  stops: [0.55, 1],
                  colors: [Colors.white, Colors.transparent],
                ).createShader(rect),
                blendMode: BlendMode.dstIn,
                child: ImageMarquee(urls: urls.isEmpty ? _fallback : urls),
              ),
            ),
          ),
          SafeArea(
            child: Column(
              children: [
                Align(
                  alignment: Alignment.centerRight,
                  child: Padding(
                    padding: const EdgeInsets.fromLTRB(0, 8, 16, 0),
                    // "Skip login" describes what the code does. "Browse
                    // the shop" describes what the person gets, which is the
                    // whole reason they opened a shopping app.
                    child: ActionButton(
                      label: 'Browse the shop',
                      primary: false,
                      onPressed: () => _enter(skip: true),
                    ),
                  ),
                ),
                const Spacer(),
                ClipRRect(
                  borderRadius: BorderRadius.circular(
                    LamazonTheme.featuredRadius,
                  ),
                  child: Image.asset(
                    'assets/logo.png',
                    width: 96,
                    height: 96,
                    fit: BoxFit.cover,
                  ),
                ),
                const SizedBox(height: 18),
                Text(
                  'Local choice. Global experience.',
                  textAlign: TextAlign.center,
                  style: LamazonTheme.titleText.copyWith(
                    fontSize: 26,
                    height: 31 / 26,
                    letterSpacing: -0.9,
                  ),
                ),
                const SizedBox(height: 22),
                Padding(
                  padding: const EdgeInsets.symmetric(horizontal: 20),
                  child: ReadableBody(
                    maxWidth: 440,
                    child: ElevatedSurface(
                      radius: LamazonTheme.featuredRadius,
                      prominent: true,
                      padding: const EdgeInsets.fromLTRB(18, 20, 18, 20),
                      child: Column(
                        children: [
                          // Says the two things are one flow, which is what
                          // stops somebody without an account hunting for a
                          // sign-up link that does not exist.
                          const Text(
                            'Log in or sign up',
                            style: LamazonTheme.sectionText,
                          ),
                          const SizedBox(height: 14),
                          // The field, the button and the line under them
                          // are the only three things a keystroke changes, so
                          // they are the only three it rebuilds.
                          //
                          // This used to be `onChanged: setState`, which
                          // rebuilt the whole sign-in screen — the animated
                          // photo backdrop included — once per character. That
                          // is wasteful rather than broken: a release build
                          // drops no characters either way, at any typing
                          // speed we could produce. Narrowing it is a frame
                          // budget fix, not a correctness one.
                          // AutofillGroup is what lets the platform commit a
                          // filled field; without one the hints above are a
                          // suggestion nothing acts on.
                          AutofillGroup(
                            child: ListenableBuilder(
                              listenable: _typedInto,
                              builder: (context, _) => Column(
                                crossAxisAlignment: CrossAxisAlignment.stretch,
                                children: [
                                  TextField(
                                    // Keyed by step, so email, code and password are
                                    // three fields rather than one field changing its
                                    // mind. A browser's password manager keys its
                                    // autofill on the field it is looking at; one
                                    // element that is an email box and then a password
                                    // box is the shape nothing can fill reliably.
                                    key: ValueKey(_step),
                                    controller: _typedInto,
                                    autofillHints: [
                                      switch (_step) {
                                        _Step.email => AutofillHints.email,
                                        _Step.code => AutofillHints.oneTimeCode,
                                        _Step.password =>
                                          AutofillHints.password,
                                      },
                                    ],
                                    keyboardType: switch (_step) {
                                      _Step.email => TextInputType.emailAddress,
                                      _Step.code => TextInputType.number,
                                      _Step.password => TextInputType.text,
                                    },
                                    obscureText: _step == _Step.password,
                                    autocorrect: false,
                                    onSubmitted: (_) {
                                      if (_valid && !_busy) _submit();
                                    },
                                    decoration: InputDecoration(
                                      // The card is already `surface`, so the field
                                      // sits a shade back from it to read as inset.
                                      fillColor: LamazonTheme.canvas,
                                      prefixIcon: Icon(
                                        switch (_step) {
                                          _Step.email => LucideIcons.mail,
                                          _Step.code => LucideIcons.keyRound,
                                          _Step.password => LucideIcons.lock,
                                        },
                                        size: 18,
                                        color: LamazonTheme.muted,
                                      ),
                                      labelText: switch (_step) {
                                        _Step.email => 'Email address',
                                        _Step.code => 'Verification code',
                                        _Step.password => 'Password',
                                      },
                                      hintText: switch (_step) {
                                        _Step.email => 'Enter email address',
                                        _Step.code => 'Enter the 6-digit code',
                                        _Step.password => 'Enter password',
                                      },
                                    ),
                                    style: const TextStyle(
                                      fontFamily: 'InterTight',
                                      fontSize: 16,
                                      fontWeight: FontWeight.w600,
                                    ),
                                  ),
                                  const SizedBox(height: 14),
                                  ActionButton(
                                    expand: true,
                                    label: _busy
                                        ? 'Please wait…'
                                        : switch (_step) {
                                            _Step.email => 'Continue',
                                            _Step.code => 'Verify code',
                                            _Step.password => 'Sign in',
                                          },
                                    loading: _busy,
                                    onPressed: _valid && !_busy
                                        ? _submit
                                        : null,
                                  ),
                                  const SizedBox(height: 12),
                                  // One line, three jobs: the failure, the reason the
                                  // button is inert, or why the address is wanted.
                                  // The middle one waits until something has been
                                  // typed — telling somebody their empty field is
                                  // invalid reads as an error before they have done
                                  // anything wrong.
                                  Semantics(
                                    liveRegion: true,
                                    child: Text(
                                      _error ??
                                          (_typed.isNotEmpty && !_valid
                                              ? switch (_step) {
                                                  _Step.email =>
                                                    'That is not an email address yet.',
                                                  _Step.code =>
                                                    'Enter the six digits from your '
                                                        'email.',
                                                  _Step.password =>
                                                    'Enter your password to sign in.',
                                                }
                                              : null) ??
                                          switch (_step) {
                                            _Step.email =>
                                              'We only use your email for order '
                                                  'updates and receipts.',
                                            _Step.code =>
                                              'We sent a code to '
                                                  '${_email.text.trim()}. It expires '
                                                  'in 10 minutes.',
                                            _Step.password =>
                                              'Signing in as ${_email.text.trim()}.',
                                          },
                                      textAlign: TextAlign.center,
                                      style: TextStyle(
                                        fontFamily: 'InterTight',
                                        fontSize: 13,
                                        height: 17 / 13,
                                        letterSpacing: 0.2,
                                        color: _error == null
                                            ? LamazonTheme.muted
                                            : LamazonTheme.danger,
                                      ),
                                    ),
                                  ),
                                ],
                              ),
                            ),
                          ),
                        ],
                      ),
                    ),
                  ),
                ),
                const SizedBox(height: 10),
                // Named and reachable. Agreeing to two documents you cannot
                // open is not agreeing to anything — and these used to be bare
                // GestureDetectors, which a keyboard could not reach and a
                // screen reader announced as ordinary text.
                //
                // The claim itself is now conditional: until both documents
                // are actually written, the app links to them without
                // asserting that anyone has agreed to them.
                Text(
                  _policiesPublished
                      ? 'By continuing, you agree to our'
                      : 'Read our',
                  textAlign: TextAlign.center,
                  style: const TextStyle(
                    fontFamily: 'InterTight',
                    fontSize: 12.5,
                    letterSpacing: 0.2,
                    color: LamazonTheme.muted,
                  ),
                ),
                const Row(
                  mainAxisAlignment: MainAxisAlignment.center,
                  children: [
                    _PolicyLink(slug: 'terms', label: 'Terms and Conditions'),
                    Text('·', style: TextStyle(color: LamazonTheme.muted)),
                    _PolicyLink(slug: 'privacy', label: 'Privacy Policy'),
                  ],
                ),
                const SizedBox(height: 8),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

// Empty on purpose. When the shop has nothing photographed yet, the backdrop
// shows nothing rather than three stock photographs of groceries the shop
// does not stock — an empty backdrop is honest, a fake one is an advert for
// products nobody can buy.
const _fallback = <String>[];

/// One policy name in the sign-in footer. A real button, so it takes keyboard
/// focus, announces itself as a control and carries a 44px target.
class _PolicyLink extends StatelessWidget {
  final String slug;
  final String label;
  const _PolicyLink({required this.slug, required this.label});

  @override
  Widget build(BuildContext context) {
    return TextButton(
      onPressed: () => Navigator.push(
        context,
        MaterialPageRoute(builder: (_) => PolicyScreen(slug: slug)),
      ),
      style: TextButton.styleFrom(
        padding: const EdgeInsets.symmetric(horizontal: 10),
        foregroundColor: LamazonTheme.strong,
      ),
      child: Text(
        label,
        style: const TextStyle(
          fontFamily: 'InterTight',
          fontSize: 12.5,
          fontWeight: FontWeight.w600,
          letterSpacing: 0.2,
          decoration: TextDecoration.underline,
        ),
      ),
    );
  }
}
