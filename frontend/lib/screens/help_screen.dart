import '../data/app_info.dart';
import 'package:flutter/material.dart';
import '../widgets/design_system.dart';

import '../widgets/app_shell.dart';
import 'package:lucide_icons_flutter/lucide_icons.dart';

import '../widgets/screen_header.dart';
import 'policy_screen.dart';

const _ink = LamazonTheme.text;
const _muted = LamazonTheme.muted;

const _faqs = [
  (
    'Where is my order?',
    'Open My Orders and tap an order for its current status, delivery address and cancellation options. Pull down to refresh.',
  ),
  (
    'How do I cancel an order?',
    'Open the order in My Orders and choose Cancel order before the shop accepts it. Accepted orders cannot be cancelled in the app.',
  ),
  (
    'When do I get my refund?',
    'The app does not process online payments or automatic refunds. Read the published Refunds policy for the applicable terms.',
  ),
  (
    'Can I change my delivery address?',
    'Yes — pick a different saved address before checkout, or add a new one '
        'from Saved Addresses.',
  ),
  (
    'Why do prices differ between shops?',
    'Each shop sets its own price. Use Compare Prices on any product to see '
        'every nearby shop selling it.',
  ),
];

class HelpScreen extends StatelessWidget {
  const HelpScreen({super.key});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: LamazonTheme.canvas,
      body: ReadableBody(
        maxWidth: 620,
        child: SafeArea(
          child: Column(
            children: [
              const ScreenHeader(title: 'Help & Support'),
              Expanded(
                child: ListView(
                  padding: const EdgeInsets.fromLTRB(20, 4, 20, 24),
                  children: [
                    Material(
                      color: Colors.white,
                      borderRadius: BorderRadius.circular(20),
                      clipBehavior: Clip.antiAlias,
                      child: Column(
                        children: [
                          _ContactTile(
                            icon: LucideIcons.mail,
                            title: 'Support information',
                            subtitle: 'View published contact details',
                          ),
                        ],
                      ),
                    ),
                    const SizedBox(height: 22),
                    const Padding(
                      padding: EdgeInsets.only(left: 4, bottom: 10),
                      child: Text(
                        'Frequently asked',
                        style: TextStyle(
                          fontSize: 16,
                          fontWeight: FontWeight.w700,
                        ),
                      ),
                    ),
                    Material(
                      color: Colors.white,
                      borderRadius: BorderRadius.circular(20),
                      clipBehavior: Clip.antiAlias,
                      child: Theme(
                        // Hide the default ExpansionTile divider lines.
                        data: Theme.of(
                          context,
                        ).copyWith(dividerColor: Colors.transparent),
                        child: Column(
                          children: [
                            for (var i = 0; i < _faqs.length; i++) ...[
                              if (i > 0)
                                const Divider(
                                  height: 1,
                                  indent: 20,
                                  color: LamazonTheme.canvas,
                                ),
                              ExpansionTile(
                                title: Text(
                                  _faqs[i].$1,
                                  style: const TextStyle(
                                    fontSize: 14,
                                    fontWeight: FontWeight.w600,
                                  ),
                                ),
                                iconColor: _ink,
                                collapsedIconColor: LamazonTheme.muted,
                                childrenPadding: const EdgeInsets.fromLTRB(
                                  20,
                                  0,
                                  20,
                                  14,
                                ),
                                expandedCrossAxisAlignment:
                                    CrossAxisAlignment.start,
                                children: [
                                  Text(
                                    _faqs[i].$2,
                                    style: const TextStyle(
                                      fontSize: 13,
                                      color: _muted,
                                      height: 1.45,
                                    ),
                                  ),
                                ],
                              ),
                            ],
                          ],
                        ),
                      ),
                    ),
                    const SizedBox(height: 22),
                    const Padding(
                      padding: EdgeInsets.only(left: 4, bottom: 10),
                      child: Text(
                        'Policies',
                        style: TextStyle(
                          fontSize: 16,
                          fontWeight: FontWeight.w700,
                        ),
                      ),
                    ),
                    const PolicyLinks(),
                    const SizedBox(height: 22),
                    Center(
                      child: Text(
                        'Unimiunte · ${AppInfo.version}',
                        style: TextStyle(
                          fontSize: 12,
                          color: LamazonTheme.muted,
                        ),
                      ),
                    ),
                  ],
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _ContactTile extends StatelessWidget {
  final IconData icon;
  final String title;
  final String subtitle;
  const _ContactTile({
    required this.icon,
    required this.title,
    required this.subtitle,
  });

  @override
  Widget build(BuildContext context) {
    return ListTile(
      leading: Icon(icon, size: 20, color: _ink),
      title: Text(
        title,
        style: const TextStyle(fontSize: 14, fontWeight: FontWeight.w600),
      ),
      subtitle: Text(
        subtitle,
        style: const TextStyle(fontSize: 12, color: _muted),
      ),
      trailing: const Icon(
        LucideIcons.chevronRight,
        size: 16,
        color: LamazonTheme.muted,
      ),
      onTap: () => Navigator.push(
        context,
        MaterialPageRoute(builder: (_) => const PolicyScreen(slug: 'contact')),
      ),
    );
  }
}
