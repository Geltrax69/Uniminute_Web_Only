import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:lucide_icons_flutter/lucide_icons.dart';
import 'package:network_image_mock/network_image_mock.dart';
import 'package:lamazon/screens/home_screen.dart';
import 'package:lamazon/widgets/design_system.dart';

/// The home header carried three controls that earned their place badly.
///
/// A hamburger opening a department drawer, when the department strip sits
/// 60px below it, a titled grid per department sits below that, and search
/// has "Shop by department" as well — four routes to the same place on one
/// screen. An account avatar going to exactly the route the always-visible
/// Account tab already goes to. And a chevron that belonged to the delivery
/// location but rendered flush against that avatar, so it read as the
/// avatar's dropdown.
///
/// This pins the header to what it is for: who you are shopping with, and
/// where the order is going.
void main() {
  testWidgets('the header offers no drawer and no duplicate account button', (
    tester,
  ) async {
    await mockNetworkImagesFor(() async {
      await tester.pumpWidget(const MaterialApp(home: HomeScreen()));
      await tester.pump();

      expect(
        find.byType(Drawer),
        findsNothing,
        reason: 'the department drawer duplicated three other surfaces',
      );
      expect(
        find.byIcon(LucideIcons.menu),
        findsNothing,
        reason: 'nothing left for a hamburger to open',
      );
      expect(
        find.widgetWithIcon(TactileIconButton, LucideIcons.userRound),
        findsNothing,
        reason: 'the Account tab is the one way to the account',
      );
    });
  });

  testWidgets('the location is still the header\'s tappable subject', (
    tester,
  ) async {
    await mockNetworkImagesFor(() async {
      await tester.pumpWidget(const MaterialApp(home: HomeScreen()));
      await tester.pump();

      // The thing the header is actually for survived the removal.
      expect(find.text('Unimiunte'), findsWidgets);
      expect(find.text('Choose delivery location'), findsOneWidget);
      // Its affordance travels with it.
      expect(find.byIcon(LucideIcons.chevronDown), findsWidgets);
    });
  });
}
