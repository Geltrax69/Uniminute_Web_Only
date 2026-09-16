import '../data/money.dart';
import 'package:flutter/material.dart';
import '../widgets/design_system.dart';

import '../data/orders.dart';
import 'package:flutter/services.dart';
import 'package:lucide_icons_flutter/lucide_icons.dart';

import '../data/api.dart';
import '../data/staff.dart';
import '../widgets/app_shell.dart';

const _ink = LamazonTheme.text;
const _muted = LamazonTheme.muted;
const _green = Color(0xFF1B7F3B);
const _red = LamazonTheme.danger;

/// The rider's panel, at /delivery. Sign in with the number an admin approved
/// and the PIN they were given; then: what is ready to collect, what is on
/// this run, and the four digits that close it.
class DeliveryScreen extends StatelessWidget {
  const DeliveryScreen({super.key});

  @override
  Widget build(BuildContext context) {
    return ListenableBuilder(
      listenable: StaffSession.rider,
      builder: (context, _) => StaffSession.rider.signedIn
          ? const _DeliveryHome()
          : const _RiderLogin(),
    );
  }
}

class _RiderLogin extends StatefulWidget {
  const _RiderLogin();

  @override
  State<_RiderLogin> createState() => _RiderLoginState();
}

class _RiderLoginState extends State<_RiderLogin> {
  final _phone = TextEditingController();
  final _pin = TextEditingController();
  bool _busy = false;
  String? _error;

  @override
  void dispose() {
    _phone.dispose();
    _pin.dispose();
    super.dispose();
  }

  Future<void> _signIn() async {
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await Api.instance.riderLogin(_phone.text.trim(), _pin.text.trim());
    } catch (e) {
      setState(
        () => _error = e.toString().replaceFirst('ClientException: ', ''),
      );
    }
    if (mounted) setState(() => _busy = false);
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: LamazonTheme.canvas,
      body: ReadableBody(
        maxWidth: 460,
        child: SafeArea(
          child: Center(
            child: SingleChildScrollView(
              padding: const EdgeInsets.all(24),
              child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  const Icon(LucideIcons.bike, size: 40, color: _green),
                  const SizedBox(height: 16),
                  const Text(
                    'Unimiunte delivery',
                    textAlign: TextAlign.center,
                    style: TextStyle(
                      fontSize: 22,
                      fontWeight: FontWeight.w800,
                      color: _ink,
                    ),
                  ),
                  const SizedBox(height: 6),
                  const Text(
                    'Only numbers an admin has approved can sign in here.',
                    textAlign: TextAlign.center,
                    style: TextStyle(fontSize: 13, color: _muted),
                  ),
                  const SizedBox(height: 24),
                  TextField(
                    controller: _phone,
                    keyboardType: TextInputType.phone,
                    decoration: _boxed('Mobile number'),
                  ),
                  const SizedBox(height: 12),
                  TextField(
                    controller: _pin,
                    keyboardType: TextInputType.number,
                    obscureText: true,
                    onSubmitted: (_) => _signIn(),
                    decoration: _boxed('PIN from your admin'),
                  ),
                  if (_error != null) ...[
                    const SizedBox(height: 12),
                    Text(
                      _error!,
                      style: const TextStyle(fontSize: 12.5, color: _red),
                    ),
                  ],
                  const SizedBox(height: 20),
                  FilledButton(
                    style: FilledButton.styleFrom(
                      backgroundColor: _green,
                      padding: const EdgeInsets.symmetric(vertical: 16),
                      shape: RoundedRectangleBorder(
                        borderRadius: BorderRadius.circular(26),
                      ),
                    ),
                    onPressed: _busy ? null : _signIn,
                    child: Text(_busy ? 'Signing in…' : 'Sign in'),
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }

  InputDecoration _boxed(String label) => InputDecoration(
    labelText: label,
    filled: true,
    fillColor: Colors.white,
    border: OutlineInputBorder(
      borderRadius: BorderRadius.circular(14),
      borderSide: BorderSide.none,
    ),
  );
}

class _DeliveryHome extends StatefulWidget {
  const _DeliveryHome();

  @override
  State<_DeliveryHome> createState() => _DeliveryHomeState();
}

class _DeliveryHomeState extends State<_DeliveryHome> {
  Map<String, dynamic>? _panel;
  Map<String, dynamic>? _history;
  String? _error;
  bool _loading = true;
  bool _showHistory = false;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() => _loading = true);
    try {
      // Both at once: the history is a second query, not a second screen, and
      // waiting for it in series would show a stale run for no reason.
      final (panel, history) = await (
        Api.instance.riderOrders(),
        Api.instance.riderHistory(),
      ).wait;
      if (mounted) {
        setState(() {
          _panel = panel;
          _history = history;
          _error = null;
        });
      }
    } catch (e) {
      if (mounted) {
        setState(
          () => _error = e.toString().replaceFirst('ClientException: ', ''),
        );
      }
    }
    if (mounted) setState(() => _loading = false);
  }

  Future<void> _pick(String id) async {
    try {
      await Api.instance.riderPick(id);
    } catch (e) {
      _say(e.toString().replaceFirst('ClientException: ', ''));
    }
    await _load();
  }

  /// The code is the hand-over. A wrong one changes nothing, which is why the
  /// error is worth showing plainly rather than as a failed request.
  Future<void> _deliver(String id) async {
    final code = TextEditingController();
    final typed = await showDialog<String>(
      context: context,
      builder: (dialog) => AlertDialog(
        backgroundColor: Colors.white,
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(20)),
        titlePadding: const EdgeInsets.fromLTRB(24, 24, 24, 0),
        contentPadding: const EdgeInsets.fromLTRB(24, 12, 24, 0),
        actionsPadding: const EdgeInsets.fromLTRB(16, 8, 16, 16),
        title: const Text(
          'Delivery code',
          style: TextStyle(fontSize: 18, fontWeight: FontWeight.w800),
        ),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text(
              'Ask the customer for their 4 digits. The order only closes if '
              'they match.',
              style: TextStyle(fontSize: 13, height: 1.4, color: _muted),
            ),
            const SizedBox(height: 20),
            // The four digits are the whole dialog, so they get typed like a
            // code rather than like a sentence: wide, spaced, centred.
            TextField(
              controller: code,
              autofocus: true,
              keyboardType: TextInputType.number,
              maxLength: 4,
              textAlign: TextAlign.center,
              inputFormatters: [FilteringTextInputFormatter.digitsOnly],
              style: const TextStyle(
                fontSize: 30,
                fontWeight: FontWeight.w700,
                letterSpacing: 14,
              ),
              decoration: InputDecoration(
                counterText: '',
                hintText: '0000',
                hintStyle: TextStyle(
                  fontSize: 30,
                  fontWeight: FontWeight.w700,
                  letterSpacing: 14,
                  color: Colors.black.withValues(alpha: 0.18),
                ),
                // letterSpacing trails after the last digit too; half of it
                // back on the left is what keeps the four looking centred.
                contentPadding: const EdgeInsets.fromLTRB(14, 16, 0, 16),
                filled: true,
                fillColor: const Color(0xFFF4F4F4),
                border: OutlineInputBorder(
                  borderRadius: BorderRadius.circular(14),
                  borderSide: BorderSide.none,
                ),
                enabledBorder: OutlineInputBorder(
                  borderRadius: BorderRadius.circular(14),
                  borderSide: BorderSide.none,
                ),
                focusedBorder: OutlineInputBorder(
                  borderRadius: BorderRadius.circular(14),
                  borderSide: const BorderSide(color: _green, width: 1.5),
                ),
              ),
              onSubmitted: (v) =>
                  v.trim().length == 4 ? Navigator.pop(dialog, v.trim()) : null,
            ),
          ],
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(dialog),
            style: TextButton.styleFrom(foregroundColor: _muted),
            child: const Text('Cancel'),
          ),
          // Enabled only on four digits: a short code is a typo, and letting
          // it through spends one of the rider's attempts on it.
          ValueListenableBuilder<TextEditingValue>(
            valueListenable: code,
            builder: (context, value, child) => FilledButton(
              style: FilledButton.styleFrom(
                backgroundColor: _green,
                padding: const EdgeInsets.symmetric(horizontal: 22),
                shape: RoundedRectangleBorder(
                  borderRadius: BorderRadius.circular(22),
                ),
              ),
              onPressed: value.text.trim().length == 4
                  ? () => Navigator.pop(dialog, code.text.trim())
                  : null,
              child: const Text(
                'Delivered',
                style: TextStyle(fontWeight: FontWeight.w700),
              ),
            ),
          ),
        ],
      ),
    );
    if (typed == null || typed.isEmpty) return;
    try {
      await Api.instance.riderDeliver(id, typed);
      _say('Delivered. The shop and the customer have been told.');
    } catch (e) {
      _say(e.toString().replaceFirst('ClientException: ', ''));
    }
    await _load();
  }

  void _say(String message) {
    if (!mounted) return;
    ScaffoldMessenger.of(context)
      ..hideCurrentSnackBar()
      ..showSnackBar(
        SnackBar(content: Text(message), behavior: SnackBarBehavior.floating),
      );
  }

  @override
  Widget build(BuildContext context) {
    final rider = _panel?['rider'] as Map<String, dynamic>?;
    final orders = (_panel?['orders'] as List<dynamic>? ?? const [])
        .cast<Map<String, dynamic>>();
    final carrying = orders.where((o) => o['stage'] == 'picked').toList();
    final waiting = orders.where((o) => o['stage'] == 'accepted').toList();

    return Scaffold(
      backgroundColor: LamazonTheme.canvas,
      body: ReadableBody(
        maxWidth: 720,
        child: SafeArea(
          child: RefreshIndicator(
            onRefresh: _load,
            child: ListView(
              padding: const EdgeInsets.fromLTRB(20, 16, 20, 40),
              children: [
                Row(
                  children: [
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            'Hello ${rider?['name']?.toString().isEmpty ?? true ? 'rider' : rider!['name']}',
                            style: const TextStyle(
                              fontSize: 22,
                              fontWeight: FontWeight.w800,
                            ),
                          ),
                          Text(
                            StaffSession.rider.subject,
                            style: const TextStyle(
                              fontSize: 12.5,
                              color: _muted,
                            ),
                          ),
                        ],
                      ),
                    ),
                    IconButton(
                      tooltip: 'Refresh',
                      onPressed: _load,
                      icon: const Icon(LucideIcons.refreshCw, size: 18),
                    ),
                    TextButton(
                      onPressed: StaffSession.rider.signOut,
                      child: const Text('Sign out'),
                    ),
                  ],
                ),
                const SizedBox(height: 12),
                Row(
                  children: [
                    _Tile(
                      label: 'On this run',
                      value: '${carrying.length}',
                      color: _green,
                    ),
                    const SizedBox(width: 10),
                    _Tile(
                      label: 'Ready to collect',
                      value: '${waiting.length}',
                    ),
                    const SizedBox(width: 10),
                    _Tile(
                      label: 'Delivered',
                      value: '${rider?['delivered'] ?? 0}',
                    ),
                  ],
                ),
                if (_error != null)
                  Padding(
                    padding: const EdgeInsets.only(top: 12),
                    child: Text(
                      _error!,
                      style: const TextStyle(color: _red, fontSize: 13),
                    ),
                  ),
                if (_loading && _panel == null)
                  const Padding(
                    padding: EdgeInsets.symmetric(vertical: 40),
                    child: Center(child: CircularProgressIndicator()),
                  ),
                const SizedBox(height: 24),
                const _Heading('On this run'),
                if (carrying.isEmpty)
                  const _Empty('Nothing picked up yet.')
                else
                  for (final o in carrying)
                    _OrderCard(
                      order: o,
                      action: 'Delivered — enter code',
                      onAction: () => _deliver(o['id'] as String),
                    ),
                const SizedBox(height: 24),
                const _Heading('Ready to collect'),
                if (waiting.isEmpty)
                  const _Empty(
                    'Nothing right now. Orders land here on their own when a '
                    'shop accepts one — pull down to check again.',
                  )
                else
                  for (final o in waiting)
                    _OrderCard(
                      order: o,
                      action: 'Picked up',
                      onAction: () => _pick(o['id'] as String),
                    ),
                const SizedBox(height: 24),
                _HistorySection(
                  history: _history,
                  expanded: _showHistory,
                  onToggle: () => setState(() => _showHistory = !_showHistory),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

/// One order, with everything a rider needs and nothing they should not have:
/// the number, the shop, and who to hand it to. Never the delivery code.
class _OrderCard extends StatelessWidget {
  final Map<String, dynamic> order;
  final String action;
  final VoidCallback onAction;
  const _OrderCard({
    required this.order,
    required this.action,
    required this.onAction,
  });

  @override
  Widget build(BuildContext context) {
    return Container(
      margin: const EdgeInsets.only(bottom: 10),
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: Colors.white,
        borderRadius: BorderRadius.circular(18),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Expanded(
                child: Text(
                  'Order No. ${orderRef(order['id'] as String)}',
                  style: const TextStyle(
                    fontSize: 14.5,
                    fontWeight: FontWeight.w800,
                  ),
                ),
              ),
              // An assigned order is yours whether or not you get there
              // first, and it is worth saying so.
              if ((order['assignedTo'] as String? ?? '').isNotEmpty)
                Container(
                  margin: const EdgeInsets.only(right: 8),
                  padding: const EdgeInsets.symmetric(
                    horizontal: 8,
                    vertical: 3,
                  ),
                  decoration: BoxDecoration(
                    color: _green.withValues(alpha: 0.12),
                    borderRadius: BorderRadius.circular(10),
                  ),
                  child: const Text(
                    'Yours',
                    style: TextStyle(
                      fontSize: 10.5,
                      fontWeight: FontWeight.w800,
                      color: _green,
                    ),
                  ),
                ),
              Text(
                '₹${(order['amount'] as num).moneyText}',
                style: const TextStyle(
                  fontSize: 14.5,
                  fontWeight: FontWeight.w800,
                  color: _green,
                ),
              ),
            ],
          ),
          const SizedBox(height: 8),
          Text(
            '${order['units']} × ${order['itemTitle']}',
            style: const TextStyle(fontSize: 13.5, fontWeight: FontWeight.w600),
          ),
          const Padding(
            padding: EdgeInsets.symmetric(vertical: 10),
            child: Divider(height: 1),
          ),
          // Both ends of the run, in the order they happen. The shop was a
          // name and nothing else, which is a place to go only if you already
          // know where it is.
          _Stop(
            icon: LucideIcons.store,
            label: 'Collect from',
            name: '${order['storeName']}',
            address: order['storeAddress'] as String? ?? '',
          ),
          const SizedBox(height: 12),
          _Stop(
            icon: LucideIcons.mapPin,
            label: 'Deliver to',
            name: '${order['receiverName']}',
            phone: '${order['receiverPhone']}',
            address: '${order['receiverAddress']}',
          ),
          const SizedBox(height: 14),
          SizedBox(
            width: double.infinity,
            child: FilledButton(
              style: FilledButton.styleFrom(
                backgroundColor: _green,
                padding: const EdgeInsets.symmetric(vertical: 14),
                shape: RoundedRectangleBorder(
                  borderRadius: BorderRadius.circular(24),
                ),
              ),
              onPressed: onAction,
              child: Text(
                action,
                style: const TextStyle(fontWeight: FontWeight.w700),
              ),
            ),
          ),
        ],
      ),
    );
  }
}

/// What this rider has already handed over. Collapsed by default: the panel
/// is for the round in front of you, and a hundred finished drops above the
/// fold would bury it.
class _HistorySection extends StatelessWidget {
  final Map<String, dynamic>? history;
  final bool expanded;
  final VoidCallback onToggle;
  const _HistorySection({
    required this.history,
    required this.expanded,
    required this.onToggle,
  });

  @override
  Widget build(BuildContext context) {
    final orders = (history?['orders'] as List<dynamic>? ?? const [])
        .cast<Map<String, dynamic>>();
    final value = (history?['value'] as num?)?.toDouble() ?? 0;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        InkWell(
          onTap: onToggle,
          borderRadius: BorderRadius.circular(10),
          child: Padding(
            padding: const EdgeInsets.symmetric(vertical: 4),
            child: Row(
              children: [
                const _Heading('Delivery history'),
                const SizedBox(width: 8),
                Icon(
                  expanded ? LucideIcons.chevronUp : LucideIcons.chevronDown,
                  size: 17,
                  color: _muted,
                ),
                const Spacer(),
                Text(
                  orders.isEmpty
                      ? 'None yet'
                      : '${orders.length} · ₹${value.moneyText}',
                  style: const TextStyle(fontSize: 12.5, color: _muted),
                ),
              ],
            ),
          ),
        ),
        if (expanded) ...[
          const SizedBox(height: 8),
          if (orders.isEmpty)
            const _Empty('Nothing delivered yet. Finished drops land here.')
          else
            for (final o in orders) _HistoryRow(order: o),
        ],
      ],
    );
  }
}

/// A finished drop, at a glance. No buttons: nothing about it can change any
/// more, and a row that cannot act should not look like one that can.
class _HistoryRow extends StatelessWidget {
  final Map<String, dynamic> order;
  const _HistoryRow({required this.order});

  @override
  Widget build(BuildContext context) {
    final when = DateTime.tryParse(order['deliveredAt'] as String? ?? '');
    return Container(
      margin: const EdgeInsets.only(bottom: 8),
      padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
      decoration: BoxDecoration(
        color: Colors.white,
        borderRadius: BorderRadius.circular(14),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Padding(
            padding: EdgeInsets.only(top: 2, right: 10),
            child: Icon(LucideIcons.circleCheckBig, size: 16, color: _green),
          ),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  '#${(order['id'] as String).toUpperCase()} · '
                  '${order['units']} × ${order['itemTitle']}',
                  style: const TextStyle(
                    fontSize: 13.5,
                    fontWeight: FontWeight.w700,
                  ),
                ),
                const SizedBox(height: 2),
                Text(
                  '${order['storeName']} → ${order['receiverName']}',
                  style: const TextStyle(fontSize: 12.5, color: _muted),
                ),
                Text(
                  '${order['receiverAddress']}',
                  maxLines: 2,
                  overflow: TextOverflow.ellipsis,
                  style: const TextStyle(
                    fontSize: 12,
                    height: 1.3,
                    color: _muted,
                  ),
                ),
              ],
            ),
          ),
          const SizedBox(width: 8),
          Column(
            crossAxisAlignment: CrossAxisAlignment.end,
            children: [
              Text(
                '₹${(order['amount'] as num).moneyText}',
                style: const TextStyle(
                  fontSize: 13.5,
                  fontWeight: FontWeight.w800,
                  color: _green,
                ),
              ),
              if (when != null)
                Text(
                  _shortDate(when.toLocal()),
                  style: const TextStyle(fontSize: 11.5, color: _muted),
                ),
            ],
          ),
        ],
      ),
    );
  }
}

const _months = [
  'Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', //
  'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec',
];

/// ponytail: "3 Aug, 18:40" by hand rather than pulling in intl for one line.
/// Swap it for DateFormat if the panel ever needs a second locale.
String _shortDate(DateTime d) {
  final hh = d.hour.toString().padLeft(2, '0');
  final mm = d.minute.toString().padLeft(2, '0');
  return '${d.day} ${_months[d.month - 1]}, $hh:$mm';
}

/// One end of the run: where to go, and who is there. The address is what the
/// rider is actually reading, so it gets the room.
class _Stop extends StatelessWidget {
  final IconData icon;
  final String label;
  final String name;
  final String? phone;
  final String address;
  const _Stop({
    required this.icon,
    required this.label,
    required this.name,
    required this.address,
    this.phone,
  });

  @override
  Widget build(BuildContext context) {
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Padding(
          padding: const EdgeInsets.only(top: 2, right: 10),
          child: Icon(icon, size: 15, color: _muted),
        ),
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                label,
                style: const TextStyle(fontSize: 11.5, color: _muted),
              ),
              const SizedBox(height: 2),
              Text(
                name,
                style: const TextStyle(
                  fontSize: 14,
                  fontWeight: FontWeight.w700,
                ),
              ),
              if (phone != null && phone!.isNotEmpty)
                Text(phone!, style: const TextStyle(fontSize: 13, color: _ink)),
              // A store with no address on file says so, rather than leaving a
              // gap the rider reads as "same as always".
              Text(
                address.isEmpty
                    ? 'Address not on file — call the shop'
                    : address,
                style: TextStyle(
                  fontSize: 13,
                  height: 1.35,
                  color: _muted,
                  fontStyle: address.isEmpty ? FontStyle.italic : null,
                ),
              ),
            ],
          ),
        ),
      ],
    );
  }
}

class _Tile extends StatelessWidget {
  final String label;
  final String value;
  final Color? color;
  const _Tile({required this.label, required this.value, this.color});

  @override
  Widget build(BuildContext context) {
    return Expanded(
      child: Container(
        padding: const EdgeInsets.symmetric(vertical: 16, horizontal: 10),
        decoration: BoxDecoration(
          color: Colors.white,
          borderRadius: BorderRadius.circular(18),
        ),
        child: Column(
          children: [
            Text(
              value,
              style: TextStyle(
                fontSize: 22,
                fontWeight: FontWeight.w800,
                color: color ?? _ink,
              ),
            ),
            const SizedBox(height: 2),
            Text(
              label,
              textAlign: TextAlign.center,
              style: const TextStyle(fontSize: 12, color: _muted),
            ),
          ],
        ),
      ),
    );
  }
}

class _Heading extends StatelessWidget {
  final String text;
  const _Heading(this.text);

  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.only(bottom: 10),
    child: Text(
      text,
      style: const TextStyle(fontSize: 16, fontWeight: FontWeight.w700),
    ),
  );
}

class _Empty extends StatelessWidget {
  final String text;
  const _Empty(this.text);

  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.only(bottom: 12),
    child: Text(text, style: const TextStyle(fontSize: 13, color: _muted)),
  );
}
