import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import '../widgets/app_nav.dart';
import 'package:flutter/material.dart';

import '../widgets/app_shell.dart';
import '../widgets/design_system.dart';
import 'package:lucide_icons_flutter/lucide_icons.dart';

import '../data/app_info.dart';
import '../data/seller.dart';
import '../data/session.dart';
import 'addresses_screen.dart';
import 'help_screen.dart';
import 'login_screen.dart';
import 'notifications_screen.dart';
import 'orders_screen.dart';
import 'seller_dashboard_screen.dart';
import 'seller_onboarding_screen.dart';
import 'settings_screen.dart';
import 'wishlist_screen.dart';

const _ink = LamazonTheme.text;
const _muted = LamazonTheme.muted;
const _green = LamazonTheme.strong;

class ProfileScreen extends StatelessWidget {
  const ProfileScreen({super.key});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      // The bar floats over the content rather than reserving a strip, which
      // is how it sits on home — bottomNavigationBar would push every screen
      // up by its height and leave a white band under it.
      extendBody: true,
      bottomNavigationBar: const SafeArea(
        child: AppBottomNav(current: AppTab.account),
      ),
      backgroundColor: LamazonTheme.canvas,
      body: ReadableBody(
        maxWidth: 620,
        child: SafeArea(
          child: ListenableBuilder(
            listenable: Listenable.merge([Session.instance, Seller.instance]),
            builder: (context, _) {
              final loggedIn = Session.instance.loggedIn;
              return ListView(
                padding: EdgeInsets.fromLTRB(
                  16,
                  8,
                  16,
                  bottomNavInset(context) + 20,
                ),
                children: [
                  Row(
                    children: [
                      GestureDetector(
                        onTap: () => Navigator.pop(context),
                        child: Container(
                          width: 46,
                          height: 46,
                          decoration: const BoxDecoration(
                            color: Colors.white,
                            shape: BoxShape.circle,
                          ),
                          child: const Icon(
                            LucideIcons.arrowLeft,
                            size: 18,
                            color: _ink,
                          ),
                        ),
                      ),
                    ],
                  ),
                  if (loggedIn) ...[
                    const SizedBox(height: 10),
                    // Who they are to us: the id they can quote, and what
                    // they can do here. Seller appears by itself the moment
                    // a store is opened.
                    Center(
                      child: Wrap(
                        spacing: 6,
                        crossAxisAlignment: WrapCrossAlignment.center,
                        children: [
                          if (Session.instance.publicId.isNotEmpty)
                            _Chip(
                              label: Session.instance.publicId,
                              color: const Color(0xFFEDEDEA),
                              text: _ink,
                            ),
                          for (final role in Session.instance.roles)
                            _Chip(
                              label: role == 'seller' ? 'Seller' : 'Buyer',
                              color: role == 'seller'
                                  ? const Color(0xFFDCEBD2)
                                  : const Color(0xFFE3ECF6),
                              text: LamazonTheme.text,
                            ),
                        ],
                      ),
                    ),
                    if (Session.instance.phone.isNotEmpty) ...[
                      const SizedBox(height: 6),
                      Center(
                        child: Text(
                          Session.instance.phone,
                          style: const TextStyle(fontSize: 13, color: _muted),
                        ),
                      ),
                    ],
                  ],
                  const SizedBox(height: 18),
                  Center(
                    child: Container(
                      width: 92,
                      height: 92,
                      decoration: const BoxDecoration(
                        color: Colors.white,
                        shape: BoxShape.circle,
                      ),
                      alignment: Alignment.center,
                      child: const Icon(
                        LucideIcons.user,
                        size: 44,
                        color: _ink,
                      ),
                    ),
                  ),
                  const SizedBox(height: 14),
                  Center(
                    child: Text(
                      // The name once we know it — asked for with the first
                      // address, so most people see their own name here.
                      loggedIn && Session.instance.name.isNotEmpty
                          ? Session.instance.name
                          : 'Your account',
                      style: const TextStyle(
                        fontSize: 24,
                        fontWeight: FontWeight.w800,
                      ),
                    ),
                  ),
                  const SizedBox(height: 4),
                  Center(
                    child: Text(
                      loggedIn
                          ? Session.instance.email!
                          : 'Log in to view your complete profile',
                      style: const TextStyle(fontSize: 14, color: _muted),
                    ),
                  ),
                  const SizedBox(height: 18),
                  if (!loggedIn)
                    OutlinedButton(
                      style: OutlinedButton.styleFrom(
                        foregroundColor: _green,
                        side: const BorderSide(color: _green),
                        padding: const EdgeInsets.symmetric(vertical: 15),
                        shape: RoundedRectangleBorder(
                          borderRadius: BorderRadius.circular(12),
                        ),
                      ),
                      onPressed: () => Navigator.push(
                        context,
                        MaterialPageRoute(builder: (_) => const LoginScreen()),
                      ),
                      child: const Text(
                        'Continue',
                        style: TextStyle(
                          fontSize: 16,
                          fontWeight: FontWeight.w700,
                        ),
                      ),
                    ),
                  const SizedBox(height: 14),
                  Row(
                    children: [
                      _Tile(
                        icon: LucideIcons.package,
                        label: 'Your orders',
                        onTap: () => Navigator.push(
                          context,
                          MaterialPageRoute(
                            builder: (_) => const OrdersScreen(),
                          ),
                        ),
                      ),
                      const SizedBox(width: 10),
                      _Tile(
                        icon: LucideIcons.messageCircle,
                        label: 'Need help?',
                        onTap: () => Navigator.push(
                          context,
                          MaterialPageRoute(builder: (_) => const HelpScreen()),
                        ),
                      ),
                    ],
                  ),
                  const SizedBox(height: 14),
                  _Card(
                    children: [
                      _Row(
                        icon: LucideIcons.settings,
                        label: 'Settings',
                        onTap: () => Navigator.push(
                          context,
                          MaterialPageRoute(
                            builder: (_) => const SettingsScreen(),
                          ),
                        ),
                      ),
                    ],
                  ),
                  const _SectionTitle('Your information'),
                  _Card(
                    children: [
                      _Row(
                        icon: LucideIcons.bookOpen,
                        label: 'Address book',
                        onTap: () => Navigator.push(
                          context,
                          MaterialPageRoute(
                            builder: (_) => const AddressesScreen(),
                          ),
                        ),
                      ),
                      _Row(
                        icon: LucideIcons.heart,
                        label: 'Saved items',
                        onTap: () => Navigator.push(
                          context,
                          MaterialPageRoute(
                            builder: (_) => const WishlistScreen(),
                          ),
                        ),
                      ),
                      _Row(
                        icon: LucideIcons.bell,
                        label: 'Notifications',
                        onTap: () => Navigator.push(
                          context,
                          MaterialPageRoute(
                            builder: (_) => const NotificationsScreen(),
                          ),
                        ),
                      ),
                    ],
                  ),
                  const _SectionTitle('Other information'),
                  _Card(
                    children: [
                      _Row(
                        icon: LucideIcons.share,
                        label: 'Share the app',
                        onTap: () async {
                          await Clipboard.setData(
                            ClipboardData(
                              text: kIsWeb
                                  ? Uri.base.origin
                                  : 'https://github.com/Geltrax69/Lamazon/releases/latest',
                            ),
                          );
                          if (context.mounted) {
                            ScaffoldMessenger.of(context).showSnackBar(
                              const SnackBar(content: Text('App link copied')),
                            );
                          }
                        },
                      ),
                      _Row(
                        icon: LucideIcons.info,
                        label: 'About us',
                        onTap: () => showAboutDialog(
                          context: context,
                          applicationName: 'Unimiunte',
                        ),
                      ),
                      _Row(
                        icon: LucideIcons.store,
                        label: _sells ? 'Your store' : 'Sell on Unimiunte',
                        onTap: () => _openSeller(context),
                      ),
                    ],
                  ),
                  if (loggedIn) ...[
                    const SizedBox(height: 14),
                    _Card(
                      children: [
                        _Row(
                          icon: LucideIcons.logOut,
                          label: 'Log out',
                          color: LamazonTheme.danger,
                          onTap: () {
                            Session.instance.signOut();
                            ScaffoldMessenger.of(context)
                              ..hideCurrentSnackBar()
                              ..showSnackBar(
                                const SnackBar(
                                  content: Text('Logged out'),
                                  behavior: SnackBarBehavior.floating,
                                  duration: Duration(seconds: 1),
                                ),
                              );
                          },
                        ),
                      ],
                    ),
                  ],
                  const SizedBox(height: 26),
                  Center(
                    child: Padding(
                      padding: const EdgeInsets.all(16),
                      child: Column(
                        children: [
                          const Text(
                            'Unimiunte',
                            style: TextStyle(
                              fontSize: 24,
                              fontWeight: FontWeight.w800,
                              color: LamazonTheme.text,
                            ),
                          ),
                          // AppInfo.load() has always run at startup and only
                          // Settings and Help showed the result. This is where
                          // somebody looks before reporting a problem.
                          if (AppInfo.version.isNotEmpty) ...[
                            const SizedBox(height: 4),
                            Text(
                              'Version ${AppInfo.version}',
                              style: LamazonTheme.mutedBodyText,
                            ),
                          ],
                        ],
                      ),
                    ),
                  ),
                ],
              );
            },
          ),
        ),
      ),
    );
  }
}

/// Sellers must be signed in, so guests sign up first and land back here.
/// With an account, an existing store goes to its dashboard and a new one
/// starts at onboarding.
Future<void> _openSeller(BuildContext context) async {
  if (!Session.instance.loggedIn) {
    await Navigator.push(
      context,
      MaterialPageRoute(builder: (_) => const LoginScreen()),
    );
    if (!context.mounted || !Session.instance.loggedIn) return;
  }
  if (!context.mounted) return;
  Navigator.push(
    context,
    MaterialPageRoute(
      builder: (_) => _sells
          ? const SellerDashboardScreen()
          : const SellerOnboardingScreen(),
    ),
  );
}

/// Whether this person has a store. The server decides — it derives the role
/// from whether one exists — and the local copy only covers the moment
/// between opening a store and the next refresh.
bool get _sells => Session.instance.isSeller || Seller.instance.hasStore;

class _Tile extends StatelessWidget {
  final IconData icon;
  final String label;
  final VoidCallback onTap;
  const _Tile({required this.icon, required this.label, required this.onTap});

  @override
  Widget build(BuildContext context) {
    return Expanded(
      child: Material(
        color: Colors.white,
        borderRadius: BorderRadius.circular(16),
        clipBehavior: Clip.antiAlias,
        child: InkWell(
          onTap: onTap,
          child: Padding(
            padding: const EdgeInsets.symmetric(vertical: 16, horizontal: 6),
            child: Column(
              children: [
                Icon(icon, size: 24, color: _ink),
                const SizedBox(height: 10),
                Text(
                  label,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: const TextStyle(
                    fontSize: 12.5,
                    fontWeight: FontWeight.w700,
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

class _SectionTitle extends StatelessWidget {
  final String text;
  const _SectionTitle(this.text);

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.fromLTRB(4, 20, 4, 10),
      child: Text(
        text,
        style: const TextStyle(fontSize: 16, fontWeight: FontWeight.w800),
      ),
    );
  }
}

class _Card extends StatelessWidget {
  final List<Widget> children;
  const _Card({required this.children});

  @override
  Widget build(BuildContext context) {
    return Material(
      color: Colors.white,
      borderRadius: BorderRadius.circular(16),
      clipBehavior: Clip.antiAlias,
      child: Column(
        children: [
          for (var i = 0; i < children.length; i++) ...[
            if (i > 0)
              const Divider(height: 1, indent: 52, color: LamazonTheme.canvas),
            children[i],
          ],
        ],
      ),
    );
  }
}

class _Row extends StatelessWidget {
  final IconData icon;
  final String label;
  final Color? color;
  final VoidCallback onTap;
  const _Row({
    required this.icon,
    required this.label,
    required this.onTap,
    this.color,
  });

  @override
  Widget build(BuildContext context) {
    return ListTile(
      leading: Icon(icon, size: 20, color: color ?? _ink),
      title: Text(
        label,
        style: TextStyle(
          fontSize: 14.5,
          fontWeight: FontWeight.w600,
          color: color ?? _ink,
        ),
      ),
      trailing: const Icon(
        LucideIcons.chevronRight,
        size: 16,
        color: LamazonTheme.muted,
      ),
      onTap: onTap,
    );
  }
}

/// A small pill: the account id, and one per role.
class _Chip extends StatelessWidget {
  final String label;
  final Color color;
  final Color text;
  const _Chip({required this.label, required this.color, required this.text});

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
      decoration: BoxDecoration(
        color: color,
        borderRadius: BorderRadius.circular(20),
      ),
      child: Text(
        label,
        style: TextStyle(
          fontSize: 11.5,
          fontWeight: FontWeight.w700,
          color: text,
        ),
      ),
    );
  }
}
