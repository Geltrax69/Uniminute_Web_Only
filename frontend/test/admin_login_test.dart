import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:lamazon/data/staff.dart';
import 'package:lamazon/screens/admin_screen.dart';
import 'package:lamazon/widgets/design_system.dart';

/// The signed-out admin panel — the sign-in card.
///
/// A debug (DDC) build used to render this as a red "Stack Overflow" box
/// where the password field should be, because the reveal toggle read
/// `LucideIcons.eye` at runtime and that class's 27,874 static consts blow
/// the stack when DDC initialises them lazily. The icons are const now, so
/// they are folded at compile time.
///
/// This test runs on the VM, which has a stack large enough not to care, so
/// it cannot catch a regression of that bug on its own — it guards the card's
/// contents. The DDC check is: `flutter run -d web-server` and open /admin.
void main() {
  testWidgets('the admin sign-in card builds', (tester) async {
    SharedPreferences.setMockInitialValues({});
    await StaffSession.admin.signOut();
    await tester.pumpWidget(
      MaterialApp(theme: LamazonTheme.data, home: const AdminScreen()),
    );
    await tester.pump();

    expect(find.text('Unimiunte admin'), findsOneWidget);
    expect(find.widgetWithText(TextField, 'Username'), findsOneWidget);
    expect(find.widgetWithText(TextField, 'Password'), findsOneWidget);
    expect(find.text('Sign in'), findsOneWidget);
    expect(tester.takeException(), isNull);

  });
}
