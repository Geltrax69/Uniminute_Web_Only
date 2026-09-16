import '../data/csv_export.dart';
import '../widgets/design_system.dart';
import '../data/money.dart';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:lucide_icons_flutter/lucide_icons.dart';

import '../data/api.dart';
import '../data/seller.dart';
import '../data/catalog.dart';
import '../data/categories.dart';
import '../models/product.dart';
import '../data/staff.dart';
import '../widgets/app_shell.dart';
import 'admin_photos_screen.dart';
import 'campaign_manager.dart';
import '../widgets/photo_picker.dart';
import '../widgets/product_card.dart';
import '../widgets/screen_header.dart';
import 'policy_screen.dart';

// Lucide icons that a widget picks between at runtime.
//
// `cond ? LucideIcons.a : LucideIcons.b` reads those statics at runtime, and
// LucideIcons is a single class holding 27,874 static consts whose lazy
// initialisation exhausts the stack in a DDC debug build — it is what turned
// the admin sign-in card into a red "Stack Overflow" box. Hoisted to consts
// here they are folded at compile time, so the initialiser never runs.
const _eye = LucideIcons.eye;
const _eyeOff = LucideIcons.eyeOff;
const _pencil = LucideIcons.pencil;
const _chevronUp = LucideIcons.chevronUp;
const _chevronDown = LucideIcons.chevronDown;

const _ink = LamazonTheme.text;
const _muted = LamazonTheme.muted;
const _green = LamazonTheme.strong;
const _amber = LamazonTheme.warning;
// The system's danger, not Google's. Also: the delete icons below sit at
// muted weight until hovered or focused — eight red trash cans in a list made
// destruction the loudest thing on a screen whose primary action is
// "+ Department".
const _red = LamazonTheme.danger;

/// The admin panel, at /admin/log_IN. Password in, and then the three things
/// an admin actually does: see who is here, decide which stores go live, and
/// hand out delivery numbers.
class AdminScreen extends StatelessWidget {
  const AdminScreen({super.key});

  @override
  Widget build(BuildContext context) {
    return ListenableBuilder(
      listenable: StaffSession.admin,
      builder: (context, _) => StaffSession.admin.signedIn
          ? const _AdminHome()
          : const _AdminLogin(),
    );
  }
}

class _AdminLogin extends StatefulWidget {
  const _AdminLogin();

  @override
  State<_AdminLogin> createState() => _AdminLoginState();
}

class _AdminLoginState extends State<_AdminLogin> {
  final _user = TextEditingController();
  final _password = TextEditingController();
  bool _busy = false;
  String? _error;

  @override
  void dispose() {
    _user.dispose();
    _password.dispose();
    super.dispose();
  }

  Future<void> _signIn() async {
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await Api.instance.adminLogin(_user.text.trim(), _password.text);
    } catch (e) {
      // The server says "wrong username or password" and nothing more, on
      // purpose — repeat it rather than guessing which half was wrong.
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
              child: ElevatedSurface(
                padding: const EdgeInsets.all(24),
                radius: LamazonTheme.featuredRadius,
                prominent: true,
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    const DecoratedBox(
                      decoration: BoxDecoration(
                        color: LamazonTheme.lime,
                        shape: BoxShape.circle,
                      ),
                      child: SizedBox(
                        width: 64,
                        height: 64,
                        child: Icon(
                          LucideIcons.shieldCheck,
                          size: 30,
                          color: LamazonTheme.strong,
                        ),
                      ),
                    ),
                    const SizedBox(height: 16),
                    const Text(
                      'Unimiunte admin',
                      textAlign: TextAlign.center,
                      style: LamazonTheme.titleText,
                    ),
                    const SizedBox(height: 6),
                    const Text(
                      'Staff only. Sellers and riders sign in elsewhere.',
                      textAlign: TextAlign.center,
                      style: LamazonTheme.mutedBodyText,
                    ),
                    const SizedBox(height: 24),
                    AutofillGroup(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.stretch,
                        children: [
                          _Field(
                            controller: _user,
                            label: 'Username',
                            autofill: const [AutofillHints.username],
                          ),
                          const SizedBox(height: 12),
                          _Field(
                            controller: _password,
                            label: 'Password',
                            obscure: true,
                            onSubmit: _signIn,
                            autofill: const [AutofillHints.password],
                          ),
                        ],
                      ),
                    ),
                    if (_error != null) ...[
                      const SizedBox(height: 12),
                      Semantics(
                        liveRegion: true,
                        child: Text(
                          _error!,
                          textAlign: TextAlign.center,
                          style: const TextStyle(
                            fontFamily: 'InterTight',
                            fontSize: 13,
                            height: 17 / 13,
                            color: LamazonTheme.danger,
                          ),
                        ),
                      ),
                    ],
                    const SizedBox(height: 20),
                    ActionButton(
                      onPressed: _busy ? null : _signIn,
                      label: _busy ? 'Signing in…' : 'Sign in',
                      expand: true,
                    ),
                  ],
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }
}

class _AdminHome extends StatefulWidget {
  const _AdminHome();

  @override
  State<_AdminHome> createState() => _AdminHomeState();
}

class _AdminHomeState extends State<_AdminHome> {
  Map<String, dynamic>? _overview;
  Map<String, dynamic>? _insights;
  List<dynamic> _stores = const [];
  List<dynamic> _riders = const [];
  List<dynamic> _orders = const [];
  List<CompareGroup> _groups = const [];

  /// Which department the Categories tab is showing the inside of. Null is
  /// the grid of them all.
  String? _openDept;

  /// The written documents, loaded with everything else.
  List<PolicyDoc> _policies = const [];

  /// Every product in the shop, whoever sells it.
  List<InventoryItem> _items = const [];

  /// Only ever the number on the Banners chip — the panel itself is owned by
  /// [CampaignManager], which loads its own.
  int _campaignCount = 0;
  _Tab _tab = _Tab.review;
  String? _error;
  bool _loading = true;
  String _search = '';
  String _stage = 'All';
  DateTimeRange? _dates;
  int _page = 0;
  static const _pageSize = 25;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() => _loading = true);
    final campaignCount = Api.instance
        .campaigns(admin: true)
        .then((rows) => rows.length)
        .catchError((_) => _campaignCount);
    try {
      // Five independent reads. In series this was five round trips of
      // waiting before anything drew.
      final (
        overview,
        insights,
        stores,
        riders,
        orders,
        groups,
        _,
        policies,
        items,
      ) = await (
        Api.instance.adminOverview(),
        Api.instance.adminInsights(),
        Api.instance.adminStores(),
        Api.instance.riders(),
        Api.instance.adminOrders(),
        Api.instance.compareGroups(),
        // Refreshes the global list the shop draws its tabs from, so adding a
        // department shows up here without a reload.
        loadDepartments(),
        // refresh: the admin is the one editing these, so a cached copy is
        // exactly the wrong thing to put in front of them.
        loadPolicies(refresh: true, admin: true),
        // The whole catalogue, every store.
        Api.instance.adminItems(),
      ).wait;
      // Started alongside the rest but awaited separately: records only wait
      // on nine futures, and this is the tenth. Only the count on the Banners
      // chip depends on it — Banners was the one destination in the panel with
      // no number beside it, which read as "this one is different" rather than
      // "nobody counted this one" — so a failure here costs a number, not the
      // whole panel.
      final campaigns = await campaignCount;
      if (!mounted) return;
      setState(() {
        _overview = overview;
        _insights = insights;
        _policies = policies;
        _stores = stores;
        _riders = riders;
        _orders = orders;
        _groups = groups;
        _items = items;
        _campaignCount = campaigns;
        _error = null;
      });
    } catch (e) {
      if (mounted) {
        setState(
          () => _error = e.toString().replaceFirst('ClientException: ', ''),
        );
      }
    }
    if (mounted) setState(() => _loading = false);
  }

  /// Reports whatever the server said, and stays quiet when it worked —
  /// the list redraws either way.
  Future<void> _itemAction(Future<void> Function() call, String done) async {
    final messenger = ScaffoldMessenger.of(context);
    try {
      await call();
      await _load();
      messenger
        ..hideCurrentSnackBar()
        ..showSnackBar(
          SnackBar(behavior: SnackBarBehavior.floating, content: Text(done)),
        );
    } catch (e) {
      messenger
        ..hideCurrentSnackBar()
        ..showSnackBar(
          SnackBar(
            behavior: SnackBarBehavior.floating,
            content: Text(e.toString().replaceFirst('ClientException: ', '')),
          ),
        );
    }
  }

  /// Deleting is irreversible and the server refuses it once the product has
  /// been ordered, so the dialog says which of those applies before asking.
  Future<void> _deleteItem(InventoryItem item) async {
    final blocked = item.orders > 0;
    final go = await showDialog<bool>(
      context: context,
      builder: (dialog) => AlertDialog(
        title: Text(
          blocked ? 'This product cannot be deleted' : 'Delete this product?',
        ),
        content: Text(
          blocked
              ? '"${item.title}" has ${item.orders == 1 ? "1 order" : "${item.orders} orders"} '
                    'against it. Deleting it would destroy those records, so the '
                    'server refuses. Hide it from the shop instead — it stops '
                    'selling and its history stays.'
              : '"${item.title}" from ${item.storeName} is removed for good, '
                    'along with its photos. This cannot be undone.',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(dialog, false),
            child: Text(blocked ? 'Close' : 'Keep it'),
          ),
          if (blocked)
            TextButton(
              onPressed: () => Navigator.pop(dialog, true),
              child: const Text('Hide it instead'),
            )
          else
            TextButton(
              onPressed: () => Navigator.pop(dialog, true),
              style: TextButton.styleFrom(foregroundColor: _red),
              child: const Text('Delete'),
            ),
        ],
      ),
    );
    if (go != true) return;
    if (blocked) {
      await _itemAction(
        () => Api.instance.adminSetDelisted(item.id, true),
        '"${item.title}" is hidden from the shop.',
      );
      return;
    }
    await _itemAction(
      () => Api.instance.adminDeleteItem(item.id),
      '"${item.title}" deleted.',
    );
  }

  Future<void> _toggleItemListing(InventoryItem item) => _itemAction(
    () => Api.instance.adminSetDelisted(item.id, !item.delisted),
    item.delisted
        ? '"${item.title}" is back on sale.'
        : '"${item.title}" is hidden from the shop.',
  );

  /// A correction, so it asks for the number rather than nudging it.
  Future<void> _editStock(InventoryItem item) async {
    final field = TextEditingController(text: '${item.stock}');
    final entered = await showDialog<String>(
      context: context,
      builder: (dialog) => AlertDialog(
        title: Text('Stock for ${item.title}'),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              '${item.reserved} of the current ${item.stock} '
              '${item.reserved == 1 ? "unit is" : "units are"} already in '
              'live orders. Shoppers can buy ${item.available}.',
              style: LamazonTheme.mutedBodyText,
            ),
            const SizedBox(height: 14),
            TextField(
              controller: field,
              autofocus: true,
              keyboardType: TextInputType.number,
              decoration: const InputDecoration(
                labelText: 'Units on the shelf',
              ),
              onSubmitted: (v) => Navigator.pop(dialog, v),
            ),
          ],
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(dialog),
            child: const Text('Cancel'),
          ),
          TextButton(
            onPressed: () => Navigator.pop(dialog, field.text),
            child: const Text('Save'),
          ),
        ],
      ),
    );
    field.dispose();
    final next = int.tryParse((entered ?? '').trim());
    if (next == null || next == item.stock) return;
    await _itemAction(
      () => Api.instance.adminSetStock(item.id, next),
      'Stock for "${item.title}" set to $next.',
    );
  }

  Future<void> _approve(String owner) async {
    final store = _stores
        .cast<Map<String, dynamic>>()
        .where((s) => s['owner'] == owner)
        .firstOrNull;
    final approved = await showDialog<bool>(
      context: context,
      builder: (dialog) => AlertDialog(
        title: const Text('Publish this store?'),
        content: Text(
          '${store?['name'] ?? owner} will be visible to shoppers and able to list products.',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(dialog, false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(dialog, true),
            child: const Text('Approve store'),
          ),
        ],
      ),
    );
    if (approved != true) return;
    try {
      await Api.instance.approveStore(owner);
      await _load();
    } catch (_) {
      if (mounted) _say('Could not approve the store. Try again.');
    }
  }

  Future<void> _reject(String owner) async {
    final reason = TextEditingController();
    final given = await showDialog<String>(
      context: context,
      builder: (dialog) => AlertDialog(
        title: const Text('Why is this store not approved?'),
        content: TextField(
          controller: reason,
          autofocus: true,
          decoration: const InputDecoration(
            hintText: 'The seller sees this, so make it actionable',
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(dialog),
            child: const Text('Cancel'),
          ),
          FilledButton(
            style: FilledButton.styleFrom(backgroundColor: _red),
            onPressed: () => Navigator.pop(dialog, reason.text.trim()),
            child: const Text('Reject'),
          ),
        ],
      ),
    );
    if (given == null || given.isEmpty) return;
    await Api.instance.rejectStore(owner, given);
    await _load();
  }

  /// A comparison group and the fields it compares on. The template is edited
  /// as a whole — a list you add to one request at a time cannot be reordered.
  Future<void> _editGroup([CompareGroup? existing]) async {
    final name = TextEditingController(text: existing?.name ?? '');
    final fields = [...?existing?.attributes];

    final ok = await showDialog<bool>(
      context: context,
      builder: (dialog) => StatefulBuilder(
        builder: (dialog, setDialog) => AlertDialog(
          title: Text(existing == null ? 'New group' : existing.name),
          content: SizedBox(
            width: 420,
            child: SingleChildScrollView(
              child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  if (existing == null)
                    TextField(
                      controller: name,
                      autofocus: true,
                      decoration: const InputDecoration(
                        labelText: 'Group',
                        hintText: 'e.g. Chargers, Thali, Shampoo',
                      ),
                    ),
                  const SizedBox(height: 16),
                  const Text(
                    'Compared on',
                    style: TextStyle(fontSize: 12.5, color: _muted),
                  ),
                  const SizedBox(height: 8),
                  for (final (i, f) in fields.indexed)
                    Padding(
                      padding: const EdgeInsets.only(bottom: 14),
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Row(
                            children: [
                              Expanded(
                                flex: 3,
                                child: TextFormField(
                                  initialValue: f.name,
                                  decoration: const InputDecoration(
                                    isDense: true,
                                    hintText: 'Field',
                                  ),
                                  onChanged: (v) =>
                                      fields[i] = fields[i].copyWith(name: v),
                                ),
                              ),
                              const SizedBox(width: 10),
                              Expanded(
                                child: TextFormField(
                                  initialValue: f.unit,
                                  decoration: const InputDecoration(
                                    isDense: true,
                                    hintText: 'Unit',
                                  ),
                                  onChanged: (v) =>
                                      fields[i] = fields[i].copyWith(unit: v),
                                ),
                              ),
                              IconButton(
                                onPressed: () =>
                                    setDialog(() => fields.removeAt(i)),
                                icon: const Icon(
                                  LucideIcons.x,
                                  size: 15,
                                  color: _muted,
                                ),
                              ),
                            ],
                          ),
                          const SizedBox(height: 6),
                          Row(
                            children: [
                              // Which way is better. Left alone, a field is
                              // shown and never ranked — which is the right
                              // answer for brand, colour and flavour.
                              Expanded(
                                child: DropdownButtonFormField<String>(
                                  isExpanded: true,
                                  initialValue: f.mode,
                                  isDense: true,
                                  decoration: const InputDecoration(
                                    isDense: true,
                                    filled: false,
                                    border: InputBorder.none,
                                    enabledBorder: InputBorder.none,
                                    focusedBorder: UnderlineInputBorder(
                                      borderSide: BorderSide(
                                        color: LamazonTheme.strong,
                                        width: 2,
                                      ),
                                    ),
                                  ),
                                  style: const TextStyle(
                                    fontSize: 12.5,
                                    color: _ink,
                                  ),
                                  items: [
                                    for (final e in CompareMode.labels.entries)
                                      DropdownMenuItem(
                                        value: e.key,
                                        child: Text(e.value),
                                      ),
                                  ],
                                  onChanged: (v) => setDialog(
                                    () => fields[i] = fields[i].copyWith(
                                      mode: v ?? CompareMode.none,
                                    ),
                                  ),
                                ),
                              ),
                              // The quantity the price is divided by. Only one
                              // can hold it: a product has one price, and a
                              // second would be the same money split another
                              // way.
                              FilterChip(
                                label: const Text('₹ per unit'),
                                labelStyle: const TextStyle(fontSize: 11.5),
                                visualDensity: VisualDensity.compact,
                                selected: f.perUnit,
                                onSelected: (on) => setDialog(() {
                                  for (final (j, g) in fields.indexed) {
                                    fields[j] = g.copyWith(
                                      perUnit: on && i == j,
                                    );
                                  }
                                }),
                              ),
                            ],
                          ),
                        ],
                      ),
                    ),
                  TextButton.icon(
                    onPressed: () =>
                        setDialog(() => fields.add(const GroupAttribute(''))),
                    icon: const Icon(LucideIcons.plus, size: 15),
                    label: const Text('Add a field'),
                  ),
                ],
              ),
            ),
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(dialog),
              child: const Text('Cancel'),
            ),
            FilledButton(
              onPressed: () => Navigator.pop(dialog, true),
              child: const Text('Save'),
            ),
          ],
        ),
      ),
    );
    if (ok != true) return;
    final groupName = existing?.name ?? name.text.trim();
    if (groupName.isEmpty) return;
    try {
      await Api.instance.saveCompareGroup(groupName, fields);
      await _load();
    } catch (e) {
      _say(e.toString().replaceFirst('ClientException: ', ''));
    }
  }

  Future<void> _deleteGroup(String name) async {
    try {
      await Api.instance.deleteCompareGroup(name);
      await _load();
    } catch (e) {
      _say(e.toString().replaceFirst('ClientException: ', ''));
    }
  }

  /// Rewrites one policy. The whole document at once, because that is how
  /// anybody edits a policy — not field by field.
  Future<void> _editPolicy(PolicyDoc doc) async {
    final saved = await Navigator.push<bool>(
      context,
      MaterialPageRoute(builder: (_) => _PolicyEditor(doc: doc)),
    );
    if (saved == true) await _load();
  }

  /// Which departments a store sells in. Order matters and is kept: the first
  /// one is the tab the shop appears under, so the admin picking Books first
  /// moves the shop there.
  Future<void> _editStoreCategories(Map<String, dynamic> store) async {
    final picked = <String>[
      ...(store['categories'] as List<dynamic>? ?? const []).cast<String>(),
    ];
    final all = [
      for (final d in departments)
        if (d.name != 'All') d.name,
    ];

    final ok = await showDialog<bool>(
      context: context,
      builder: (dialog) => StatefulBuilder(
        builder: (dialog, setDialog) => AlertDialog(
          title: Text('${store['name']} sells in'),
          content: SingleChildScrollView(
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  picked.isEmpty
                      ? 'Pick at least one.'
                      : 'Shown under ${picked.first}. Tap to reorder — the '
                            'first one is the tab it appears on.',
                  style: const TextStyle(fontSize: 12.5, color: _muted),
                ),
                const SizedBox(height: 14),
                Wrap(
                  spacing: 8,
                  runSpacing: 8,
                  children: [
                    for (final name in all)
                      _PickChip(
                        label: name,
                        // The number says where it sits in the order, which is
                        // the only thing that makes "first one wins" visible.
                        rank: picked.indexOf(name),
                        onTap: () => setDialog(() {
                          if (picked.contains(name)) {
                            // Already first: tapping again is how you remove
                            // it, so a mis-tap is one tap to undo.
                            if (picked.first == name) {
                              picked.remove(name);
                            } else {
                              picked
                                ..remove(name)
                                ..insert(0, name);
                            }
                          } else {
                            picked.add(name);
                          }
                        }),
                      ),
                  ],
                ),
              ],
            ),
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(dialog),
              child: const Text('Cancel'),
            ),
            FilledButton(
              onPressed: picked.isEmpty
                  ? null
                  : () => Navigator.pop(dialog, true),
              child: const Text('Save'),
            ),
          ],
        ),
      ),
    );
    if (ok != true) return;
    try {
      await Api.instance.setStoreCategories(store['owner'] as String, picked);
      await _load();
    } catch (e) {
      _say(e.toString().replaceFirst('ClientException: ', ''));
    }
  }

  /// Adds a department, or a category inside one. A department gets an icon
  /// and a colour because it has a tab to draw; a category is just a name.
  Future<void> _addCategory({String parent = ''}) async {
    final name = TextEditingController();
    var icon = departmentIcons.keys.first;
    var colour = _palette.first;
    Uint8List? photo;

    final ok = await showDialog<bool>(
      context: context,
      builder: (dialog) => StatefulBuilder(
        builder: (dialog, setDialog) => AlertDialog(
          title: Text(
            parent.isEmpty ? 'New department' : 'New category in $parent',
          ),
          content: SingleChildScrollView(
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                TextField(
                  controller: name,
                  autofocus: true,
                  decoration: InputDecoration(
                    labelText: 'Name',
                    hintText: parent.isEmpty ? 'e.g. Stationery' : 'e.g. Pens',
                  ),
                ),
                const SizedBox(height: 18),
                const Text(
                  'Picture',
                  style: TextStyle(fontSize: 12.5, color: _muted),
                ),
                const SizedBox(height: 8),
                // Square, because that is the shape the shop's category grid
                // draws it in. Optional: without one the tile falls back to
                // the icon, which is all this had before.
                PhotoTile(
                  photo: photo,
                  aspect: 1,
                  height: 120,
                  emptyLabel: 'Add a picture',
                  emptyHint: 'Shown on the shop’s category grid',
                  onChanged: (bytes) => setDialog(() => photo = bytes),
                ),
                if (parent.isEmpty) ...[
                  const SizedBox(height: 18),
                  const Text(
                    'Icon',
                    style: TextStyle(fontSize: 12.5, color: _muted),
                  ),
                  const SizedBox(height: 8),
                  // A fixed set, not a text field: Flutter drops any icon it
                  // cannot see referenced in the code, so a name typed here
                  // would render an empty box in the shop.
                  Wrap(
                    spacing: 8,
                    runSpacing: 8,
                    children: [
                      for (final entry in departmentIcons.entries)
                        GestureDetector(
                          onTap: () => setDialog(() => icon = entry.key),
                          child: Container(
                            width: 40,
                            height: 40,
                            alignment: Alignment.center,
                            decoration: BoxDecoration(
                              color: icon == entry.key
                                  ? _ink
                                  : const Color(0xFFEDEDE9),
                              borderRadius: BorderRadius.circular(12),
                            ),
                            child: Icon(
                              entry.value,
                              size: 18,
                              color: icon == entry.key ? Colors.white : _ink,
                            ),
                          ),
                        ),
                    ],
                  ),
                  const SizedBox(height: 18),
                  const Text(
                    'Colour',
                    style: TextStyle(fontSize: 12.5, color: _muted),
                  ),
                  const SizedBox(height: 8),
                  Wrap(
                    spacing: 8,
                    runSpacing: 8,
                    children: [
                      for (final hex in _palette)
                        GestureDetector(
                          onTap: () => setDialog(() => colour = hex),
                          child: Container(
                            width: 34,
                            height: 34,
                            decoration: BoxDecoration(
                              color: _hex(hex),
                              shape: BoxShape.circle,
                              border: Border.all(
                                color: colour == hex ? _ink : Colors.black12,
                                width: colour == hex ? 2.5 : 1,
                              ),
                            ),
                          ),
                        ),
                    ],
                  ),
                ],
              ],
            ),
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(dialog),
              child: const Text('Cancel'),
            ),
            FilledButton(
              onPressed: () => Navigator.pop(dialog, true),
              child: const Text('Add'),
            ),
          ],
        ),
      ),
    );
    if (ok != true || name.text.trim().isEmpty) return;
    try {
      await Api.instance.addCategory(
        name: name.text.trim(),
        parent: parent,
        icon: parent.isEmpty ? icon : '',
        colour: parent.isEmpty ? colour : '',
      );
      // The row first, then its picture: the upload needs a row to hang the
      // URL on, and a failed upload leaves a category that simply has no
      // picture yet rather than no category.
      if (photo != null) {
        await Api.instance.setCategoryPhoto(name.text.trim(), photo!);
      }
      await _load();
    } catch (e) {
      _say(e.toString().replaceFirst('ClientException: ', ''));
    }
  }

  /// Adds or replaces the picture of a category that already exists. One name
  /// per category in Cloudinary, so this overwrites rather than piles up.
  Future<void> _setCategoryPhoto(String name) async {
    final picked = await pickPhotos(multiple: false);
    if (picked.isEmpty) return;
    // Straight up as picked: it is squared on delivery, so making an admin
    // crop a picture to a shape we can produce ourselves is busywork.
    try {
      await Api.instance.setCategoryPhoto(name, picked.first);
      await _load();
    } catch (e) {
      _say(e.toString().replaceFirst('ClientException: ', ''));
    }
  }

  /// Deleting asks first, and the server refuses when stock or categories are
  /// still filed under it — so the confirmation is about intent, and the
  /// error that may follow is about consequence.
  Future<void> _deleteCategory(String name) async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (dialog) => AlertDialog(
        title: Text('Remove $name?'),
        content: const Text(
          'It disappears from the shop and sellers can no longer list under '
          'it. Anything already filed under it has to be moved first.',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(dialog),
            child: const Text('Cancel'),
          ),
          FilledButton(
            style: FilledButton.styleFrom(backgroundColor: _red),
            onPressed: () => Navigator.pop(dialog, true),
            child: const Text('Remove'),
          ),
        ],
      ),
    );
    if (ok != true) return;
    try {
      await Api.instance.deleteCategory(name);
      await _load();
    } catch (e) {
      _say(e.toString().replaceFirst('ClientException: ', ''));
    }
  }

  /// The PIN exists for exactly one moment — this dialog. It is not stored in
  /// readable form anywhere, so an admin who closes this hands out a new one.
  Future<void> _addRider() async {
    final phone = TextEditingController();
    final name = TextEditingController();
    final ok = await showDialog<bool>(
      context: context,
      builder: (dialog) => AlertDialog(
        title: const Text('Add a delivery number'),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            TextField(
              controller: phone,
              autofocus: true,
              keyboardType: TextInputType.phone,
              decoration: const InputDecoration(labelText: 'Mobile number'),
            ),
            TextField(
              controller: name,
              decoration: const InputDecoration(labelText: 'Name'),
            ),
          ],
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(dialog),
            child: const Text('Cancel'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(dialog, true),
            child: const Text('Add'),
          ),
        ],
      ),
    );
    if (ok != true) return;
    try {
      final pin = await Api.instance.addRider(phone.text, name.text);
      if (!mounted) return;
      await _showPIN(pin, phone.text.trim());
      await _load();
    } catch (e) {
      if (!mounted) return;
      ScaffoldMessenger.of(context)
        ..hideCurrentSnackBar()
        ..showSnackBar(
          SnackBar(
            content: Text(e.toString().replaceFirst('ClientException: ', '')),
          ),
        );
    }
  }

  /// The PIN exists for one moment: this dialog. Nothing stores it in
  /// readable form, so an admin who closes this issues another one.
  Future<void> _showPIN(String pin, String phone) => showDialog<void>(
    context: context,
    builder: (dialog) => AlertDialog(
      title: const Text('Give them this PIN'),
      content: Text(
        '$pin\n\nThey sign in at /delivery with $phone and this PIN. '
        'It is shown once — issuing another replaces it.',
        style: const TextStyle(fontSize: 15, height: 1.5),
      ),
      actions: [
        FilledButton(
          onPressed: () => Navigator.pop(dialog),
          child: const Text('Done'),
        ),
      ],
    ),
  );

  Future<void> _resetPin(Map<String, dynamic> rider) async {
    final phone = rider['phone'] as String;
    final ok = await _confirm(
      'New PIN for $phone?',
      'Their old PIN stops working straight away, and they are signed out of '
          'the delivery panel until they use the new one.',
      'Issue PIN',
    );
    if (!ok) return;
    try {
      final pin = await Api.instance.resetRiderPin(phone);
      if (!mounted) return;
      await _showPIN(pin, phone);
    } catch (e) {
      _say(e.toString().replaceFirst('ClientException: ', ''));
    }
    await _load();
  }

  /// The same rider on a new SIM. Everything of theirs is keyed by the
  /// number, so the server moves the run and the history together — this
  /// side only has to ask for the new one.
  Future<void> _changeNumber(Map<String, dynamic> rider) async {
    final old = rider['phone'] as String;
    final next = TextEditingController();
    final ok = await showDialog<bool>(
      context: context,
      builder: (dialog) => AlertDialog(
        title: Text('Move $old to a new number'),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text(
              'Their deliveries, their count and anything in their hand right '
              'now come with them. They get a new PIN.',
              style: TextStyle(fontSize: 13, color: _muted),
            ),
            const SizedBox(height: 12),
            TextField(
              controller: next,
              autofocus: true,
              keyboardType: TextInputType.phone,
              decoration: const InputDecoration(labelText: 'New mobile number'),
            ),
          ],
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(dialog),
            child: const Text('Cancel'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(dialog, true),
            child: const Text('Move'),
          ),
        ],
      ),
    );
    if (ok != true) return;
    try {
      final pin = await Api.instance.changeRiderNumber(old, next.text.trim());
      if (!mounted) return;
      await _showPIN(pin, next.text.trim());
    } catch (e) {
      _say(e.toString().replaceFirst('ClientException: ', ''));
    }
    await _load();
  }

  /// Off is a pause, not an ending: no new orders, signed out, everything
  /// they have done still theirs. On again needs no new PIN.
  Future<void> _switchRider(Map<String, dynamic> rider) async {
    final phone = rider['phone'] as String;
    final on = rider['active'] == true;
    if (on &&
        !await _confirm(
          'Switch off $phone?',
          'They stop being handed orders and are signed out of the delivery '
              'panel. Their deliveries stay on their name, and you can switch '
              'them back on with the same PIN.',
          'Switch off',
        )) {
      return;
    }
    try {
      on
          ? await Api.instance.removeRider(phone)
          : await Api.instance.restoreRider(phone);
    } catch (e) {
      _say(e.toString().replaceFirst('ClientException: ', ''));
    }
    await _load();
  }

  Future<void> _deleteRider(Map<String, dynamic> rider) async {
    final phone = rider['phone'] as String;
    if (!await _confirm(
      'Remove $phone for good?',
      'The rider is deleted. Anything they were assigned but had not '
          'collected goes back to the other riders. This cannot be undone — '
          'switch them off instead if they might come back.',
      'Remove',
    )) {
      return;
    }
    try {
      await Api.instance.deleteRider(phone);
    } catch (e) {
      // The server refuses while a bag is in their hand, and says so.
      _say(e.toString().replaceFirst('ClientException: ', ''));
    }
    await _load();
  }

  Future<bool> _confirm(String title, String body, String action) async =>
      await showDialog<bool>(
        context: context,
        builder: (dialog) => AlertDialog(
          title: Text(title),
          content: Text(
            body,
            style: const TextStyle(fontSize: 13.5, height: 1.4),
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(dialog),
              child: const Text('Cancel'),
            ),
            FilledButton(
              onPressed: () => Navigator.pop(dialog, true),
              child: Text(action),
            ),
          ],
        ),
      ) ??
      false;

  /// Orders hand themselves to a rider when the shop accepts them, so this is
  /// the exception: the rider who did not turn up. Only orders nobody has
  /// picked up can be moved — after that the bag is physically with someone
  /// and the database says so.
  Future<void> _assign(Map<String, dynamic> order) async {
    final active = _riders.where((r) => r['active'] == true).toList();
    if (active.isEmpty) {
      _say('Add a delivery number first.');
      return;
    }
    final chosen = await showDialog<String>(
      context: context,
      builder: (dialog) => SimpleDialog(
        title: Text('Who takes #${(order['id'] as String).toUpperCase()}?'),
        children: [
          for (final r in active)
            SimpleDialogOption(
              onPressed: () => Navigator.pop(dialog, r['phone'] as String),
              child: Text(
                '${(r['name'] as String?)?.isEmpty ?? true ? 'Rider' : r['name']} · '
                '${r['phone']}  (${r['carrying']} carrying)',
              ),
            ),
          const Divider(),
          SimpleDialogOption(
            onPressed: () => Navigator.pop(dialog, ''),
            child: const Text('Leave it open to any rider'),
          ),
        ],
      ),
    );
    if (chosen == null) return;
    try {
      await Api.instance.assignOrder(order['id'] as String, chosen);
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
    final o = _overview;
    final counts = _counts();
    return Scaffold(
      backgroundColor: LamazonTheme.canvas,
      body: ReadableBody(
        maxWidth: 1280,
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
                          Semantics(
                            headingLevel: 1,
                            child: const Text(
                              'Admin',
                              style: LamazonTheme.titleText,
                            ),
                          ),
                          // Who is about to approve a store or delete a
                          // department. The panel offered "Sign out" without
                          // ever saying who was being signed out.
                          if (StaffSession.admin.subject.isNotEmpty)
                            Text(
                              'Signed in as ${StaffSession.admin.subject}',
                              style: LamazonTheme.mutedBodyText,
                            ),
                        ],
                      ),
                    ),
                    TactileIconButton(
                      label: 'Refresh',
                      onPressed: _load,
                      icon: LucideIcons.refreshCw,
                      size: 40,
                    ),
                    const SizedBox(width: 8),
                    ActionButton(
                      onPressed: StaffSession.admin.signOut,
                      label: 'Sign out',
                      primary: false,
                    ),
                  ],
                ),
                if (_error != null)
                  Padding(
                    padding: const EdgeInsets.only(bottom: 12),
                    child: Text(
                      _error!,
                      style: const TextStyle(color: _red, fontSize: 13),
                    ),
                  ),
                if (_loading && o == null)
                  const Padding(
                    padding: EdgeInsets.symmetric(vertical: 40),
                    child: Center(child: CircularProgressIndicator()),
                  ),
                if (o != null) _kpis(o, counts),
                const SizedBox(height: 22),
                // One list at a time. Six sections stacked down one page meant
                // scrolling past everything to reach anything.
                if (MediaQuery.sizeOf(context).width < 700)
                  DropdownButtonFormField<_Tab>(
                    isExpanded: true,
                    key: ValueKey(_tab),
                    initialValue: _tab,
                    decoration: const InputDecoration(
                      labelText: 'Admin section',
                    ),
                    items: [
                      for (final tab in _Tab.values)
                        DropdownMenuItem(
                          value: tab,
                          child: Text('${tab.label} (${counts[tab]})'),
                        ),
                    ],
                    onChanged: (value) {
                      if (value != null) _show(value);
                    },
                  )
                else ...[
                  // Named, and split in two. Eleven identical chips in one row
                  // said nothing about what any of them did: three of them are
                  // three views of the stores list — a filter — and the other
                  // eight are destinations, and nothing on screen distinguished
                  // the two. They are still chips, still in the same place, and
                  // still do exactly what they did.
                  _ChipGroup(
                    label: 'Stores',
                    semanticLabel: 'Store list',
                    chips: [
                      for (final tab in _storeTabs) _sectionChip(tab, counts),
                    ],
                  ),
                  const SizedBox(height: 10),
                  _ChipGroup(
                    label: 'Manage',
                    semanticLabel: 'Admin section',
                    chips: [
                      for (final tab in _Tab.values)
                        if (!_storeTabs.contains(tab))
                          _sectionChip(tab, counts),
                    ],
                  ),
                ],
                const SizedBox(height: 20),
                // Only when there is something to search or filter. A search
                // box, a status dropdown and a date range above an empty list
                // are three controls that cannot do anything.
                if (_hasTable && _activeRows.isNotEmpty) ..._tableControls(),
                ..._section(),
                if (_hasTable) _pagination(),
              ],
            ),
          ),
        ),
      ),
    );
  }

  void _show(_Tab tab) => setState(() {
    _tab = tab;
    _search = '';
    _stage = 'All';
    _dates = null;
    _page = 0;
  });

  bool get _hasTable => {
    _Tab.products,
    _Tab.review,
    _Tab.approved,
    _Tab.rejected,
    _Tab.orders,
    _Tab.delivery,
    _Tab.people,
  }.contains(_tab);
  List<Map<String, dynamic>> get _activeRows => switch (_tab) {
    _Tab.review => _storesWith('pending'),
    _Tab.approved => _storesWith('approved'),
    _Tab.rejected => _storesWith('rejected'),
    _Tab.orders => _orders.cast<Map<String, dynamic>>(),
    _Tab.products => _items.map((i) => {'item': i}).toList(),
    _Tab.delivery => _riders.cast<Map<String, dynamic>>(),
    _Tab.people =>
      (_overview?['people'] as List<dynamic>? ?? [])
          .cast<Map<String, dynamic>>(),
    _ => [],
  };
  List<Map<String, dynamic>> _filtered(List<Map<String, dynamic>> rows) =>
      rows.where((row) {
        if (_search.isNotEmpty &&
            !row.values
                .join(' ')
                .toLowerCase()
                .contains(_search.toLowerCase())) {
          return false;
        }
        if (_tab == _Tab.orders && _stage != 'All' && row['stage'] != _stage) {
          return false;
        }
        if (_dates != null && _tab == _Tab.orders) {
          final date = DateTime.tryParse(
            row['placedAt']?.toString() ?? '',
          )?.toLocal();
          if (date == null ||
              date.isBefore(_dates!.start) ||
              !date.isBefore(_dates!.end.add(const Duration(days: 1)))) {
            return false;
          }
        }
        return true;
      }).toList();
  List<Map<String, dynamic>> _visible(List<Map<String, dynamic>> rows) {
    final filtered = _filtered(rows);
    final page = _page.clamp(
      0,
      filtered.isEmpty ? 0 : (filtered.length - 1) ~/ _pageSize,
    );
    return filtered.skip(page * _pageSize).take(_pageSize).toList();
  }

  List<Widget> _tableControls() => [
    ElevatedSurface(
      radius: LamazonTheme.smallRadius,
      child: TextField(
        key: ValueKey('search-$_tab'),
        decoration: InputDecoration(
          labelText: 'Search ${_tab.label.toLowerCase()}',
          prefixIcon: const Icon(LucideIcons.search),
        ),
        onChanged: (value) => setState(() {
          _search = value.trim();
          _page = 0;
        }),
      ),
    ),
    const SizedBox(height: 12),
    Wrap(
      spacing: 12,
      runSpacing: 8,
      crossAxisAlignment: WrapCrossAlignment.center,
      children: [
        if (_tab == _Tab.orders)
          SizedBox(
            width: 190,
            child: DropdownButtonFormField<String>(
              isExpanded: true,
              initialValue: _stage,
              decoration: const InputDecoration(labelText: 'Order status'),
              items: [
                for (final status in [
                  'All',
                  'received',
                  'accepted',
                  'picked',
                  'delivered',
                  'rejected',
                ])
                  DropdownMenuItem(
                    value: status,
                    child: Text(status == 'All' ? 'All statuses' : status),
                  ),
              ],
              onChanged: (value) => setState(() {
                _stage = value!;
                _page = 0;
              }),
            ),
          ),
        if (_tab == _Tab.orders)
          ActionButton(
            icon: LucideIcons.calendar,
            label: _dates == null
                ? 'Date range'
                : '${_dates!.start.day}/${_dates!.start.month} – ${_dates!.end.day}/${_dates!.end.month}',
            primary: false,
            onPressed: () async {
              final picked = await showDateRangePicker(
                context: context,
                firstDate: DateTime(2020),
                lastDate: DateTime.now(),
                initialDateRange: _dates,
              );
              if (picked != null && mounted) {
                setState(() {
                  _dates = picked;
                  _page = 0;
                });
              }
            },
          ),
        if (_dates != null)
          TextButton(
            onPressed: () => setState(() {
              _dates = null;
              _page = 0;
            }),
            child: const Text('Clear dates'),
          ),
        ActionButton(
          icon: LucideIcons.download,
          label: 'Export CSV',
          primary: false,
          onPressed: _filtered(_activeRows).isEmpty
              ? null
              : () async {
                  try {
                    final message = await exportCsv(
                      'lamazon-${_tab.name}.csv',
                      rowsToCsv(_filtered(_activeRows)),
                    );
                    if (mounted) {
                      ScaffoldMessenger.of(
                        context,
                      ).showSnackBar(SnackBar(content: Text(message)));
                    }
                  } catch (_) {
                    if (mounted) {
                      ScaffoldMessenger.of(context).showSnackBar(
                        const SnackBar(
                          content: Text('Export failed. Try again.'),
                        ),
                      );
                    }
                  }
                },
        ),
      ],
    ),
    const SizedBox(height: 16),
    if (_filtered(_activeRows).isEmpty)
      const Padding(
        padding: EdgeInsets.all(20),
        child: Text('No matching records. Try another search or filter.'),
      ),
  ];

  /// The six counts, as one band rather than two.
  ///
  /// These sit above every section, not just an overview, so their height is
  /// paid on each one — two rows of three took about 150px of a 800px
  /// viewport before the admin could see any actual work. Six across on a
  /// desktop halves that; a phone keeps the three-up reflow, which was
  /// already the best responsive behaviour in the app.
  ///
  /// The numbers are also the way in: tapping one opens the list behind it,
  /// so a count is never a dead end.
  Widget _kpis(Map<String, dynamic> o, Map<_Tab, int> counts) {
    final tiles = <Widget>[
      _Tile(
        label: 'People',
        value: '${o['users']}',
        onTap: () => _show(_Tab.people),
      ),
      _Tile(
        label: 'Sellers',
        value: '${o['sellers']}',
        onTap: () => _show(_Tab.approved),
      ),
      _Tile(
        label: 'To review',
        value: '${counts[_Tab.review]}',
        color: counts[_Tab.review]! > 0 ? _amber : null,
        onTap: () => _show(_Tab.review),
      ),
      _Tile(
        label: 'Riders',
        value: '${o['riders']}',
        onTap: () => _show(_Tab.delivery),
      ),
      _Tile(
        label: 'Orders',
        value: '${o['orders']}',
        onTap: () => _show(_Tab.orders),
      ),
      _Tile(
        label: 'Rejected',
        value: '${counts[_Tab.rejected]}',
        onTap: () => _show(_Tab.rejected),
      ),
    ];
    if (isWide(context)) {
      return Row(
        children: [
          for (var i = 0; i < tiles.length; i++) ...[
            if (i > 0) const SizedBox(width: 10),
            tiles[i],
          ],
        ],
      );
    }
    return Column(
      children: [
        for (var start = 0; start < tiles.length; start += 3) ...[
          if (start > 0) const SizedBox(height: 10),
          Row(
            children: [
              for (var i = start; i < start + 3 && i < tiles.length; i++) ...[
                if (i > start) const SizedBox(width: 10),
                tiles[i],
              ],
            ],
          ),
        ],
      ],
    );
  }

  Widget _pagination() {
    final total = _filtered(_activeRows).length;
    // Nothing at all: the empty state above has already said so, and
    // "0 records" under it was the third message in a stack of three.
    if (total == 0) return const SizedBox.shrink();
    final page = _page.clamp(0, (total - 1) ~/ _pageSize);
    // The count is worth showing on any result set — it is how you tell a
    // search worked. The buttons are not: Previous and Next beside a single
    // page are two controls that can never do anything.
    final paged = total > _pageSize;
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 20),
      child: Wrap(
        spacing: 12,
        crossAxisAlignment: WrapCrossAlignment.center,
        children: [
          Text(
            '${page * _pageSize + 1}–${((page + 1) * _pageSize).clamp(0, total)} of $total',
          ),
          if (paged) ...[
            ActionButton(
              onPressed: page == 0
                  ? null
                  : () => setState(() => _page = page - 1),
              label: 'Previous',
              primary: false,
            ),
            ActionButton(
              onPressed: (page + 1) * _pageSize >= total
                  ? null
                  : () => setState(() => _page = page + 1),
              label: 'Next',
              primary: false,
            ),
          ],
        ],
      ),
    );
  }

  List<Map<String, dynamic>> _storesWith(String status) => _stores
      .cast<Map<String, dynamic>>()
      .where((s) => s['status'] == status)
      .toList();

  /// One destination, as a chip. Merged rather than excluded: the chip's own
  /// node is what carries role and tab stop, and excluding it to stop the
  /// label being said twice left every section unreachable by keyboard.
  Widget _sectionChip(_Tab tab, Map<_Tab, int> counts) => MergeSemantics(
    child: Semantics(
      // ChoiceChip announces role=checkbox, which says "tick any of these".
      // These are destinations and exactly one is current.
      inMutuallyExclusiveGroup: true,
      selected: _tab == tab,
      child: ChoiceChip(
        label: Text('${tab.label} (${counts[tab]})'),
        selected: _tab == tab,
        onSelected: (_) => _show(tab),
        selectedColor: LamazonTheme.accent,
        backgroundColor: LamazonTheme.surface,
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(20)),
      ),
    ),
  );

  Map<_Tab, int> _counts() => {
    _Tab.review: _storesWith('pending').length,
    _Tab.approved: _storesWith('approved').length,
    _Tab.rejected: _storesWith('rejected').length,
    _Tab.orders: _orders.length,
    _Tab.products: _items.length,
    _Tab.insights: (_insights?['topStores'] as List?)?.length ?? 0,
    _Tab.banners: _campaignCount,
    _Tab.categories: departments.where((d) => d.name != 'All').length,
    _Tab.policies: _policies.length,
    _Tab.compare: _groups.length,
    _Tab.delivery: _riders.length,
    _Tab.people: (_overview?['people'] as List?)?.length ?? 0,
  };

  /// What the chosen button shows. Each branch is one list plus the line of
  /// context it needs — the rules that are not visible in the rows themselves.
  List<Widget> _section() {
    switch (_tab) {
      case _Tab.banners:
        return [const CampaignManager()];
      case _Tab.review:
        final pending = _storesWith('pending');
        return [
          const _Note(
            'A store stays invisible to shoppers, and its owner '
            'cannot add stock, until it is approved here.',
          ),
          if (pending.isEmpty)
            const _Empty('Nothing waiting. Every store has been looked at.')
          else
            for (final s in _visible(pending))
              _StoreCard(
                store: s,
                onApprove: () => _approve(s['owner'] as String),
                onReject: () => _reject(s['owner'] as String),
                onEditCategories: () => _editStoreCategories(s),
              ),
        ];

      case _Tab.approved:
        final live = _storesWith('approved');
        return [
          const _Note('Live on the shop page and taking orders.'),
          if (live.isEmpty)
            const _Empty('No approved stores yet.')
          else
            for (final s in _visible(live))
              _StoreCard(
                store: s,
                onReject: () => _reject(s['owner'] as String),
                onEditCategories: () => _editStoreCategories(s),
              ),
        ];

      case _Tab.rejected:
        final out = _storesWith('rejected');
        return [
          const _Note(
            'The seller sees the reason and can edit their store to '
            'send it back for another look.',
          ),
          if (out.isEmpty)
            const _Empty('Nothing rejected.')
          else
            for (final s in _visible(out))
              _StoreCard(
                store: s,
                onApprove: () => _approve(s['owner'] as String),
                onEditCategories: () => _editStoreCategories(s),
              ),
        ];

      case _Tab.orders:
        return [
          const _Note(
            'Accepted orders go to a rider automatically — whoever '
            'is carrying the least, picked at random between equals. '
            'Reassign only when someone does not turn up.',
          ),
          if (_orders.isEmpty)
            const _Empty('No orders yet.')
          else
            for (final ord in _visible(_orders.cast<Map<String, dynamic>>()))
              _AdminOrderRow(
                order: ord,
                riders: _riders,
                onAssign: () => _assign(ord),
              ),
        ];

      case _Tab.insights:
        final stores = (_insights?['topStores'] as List<dynamic>? ?? const [])
            .cast<Map<String, dynamic>>();
        final items = (_insights?['topItems'] as List<dynamic>? ?? const [])
            .cast<Map<String, dynamic>>();
        final totals =
            _insights?['totals'] as Map<String, dynamic>? ?? const {};
        return [
          const _Note(
            'Rejected orders are left out: a shop turning work away is '
            'not a shop selling. Revenue counts delivered orders only.',
          ),
          Row(
            children: [
              _Stat(label: 'Orders placed', value: '${totals['placed'] ?? 0}'),
              const SizedBox(width: 10),
              _Stat(
                label: 'Delivered',
                value: '${totals['delivered'] ?? 0}',
                color: _green,
              ),
              const SizedBox(width: 10),
              _Stat(
                label: 'Revenue',
                value: '₹${((totals['revenue'] as num?) ?? 0).moneyText}',
              ),
            ],
          ),
          const SizedBox(height: 22),
          const _InsightHeading('Stores by orders'),
          if (stores.isEmpty)
            const _Empty('No orders yet, so nothing to rank.')
          else
            for (final (i, s) in stores.indexed)
              _RankRow(
                rank: i + 1,
                title: s['name'] as String? ?? '',
                subtitle: '${s['units']} units · ${s['delivered']} delivered',
                trailing: '${s['orders']} orders',
                note: '₹${((s['revenue'] as num?) ?? 0).moneyText}',
                // The bar is read against the top row, not against a total:
                // "half of what the leader does" is the comparison an admin
                // actually makes.
                fraction: _share(s['orders'], stores.first['orders']),
              ),
          const SizedBox(height: 22),
          const _InsightHeading('Most ordered items'),
          if (items.isEmpty)
            const _Empty('No orders yet, so nothing to rank.')
          else
            for (final (i, it) in items.indexed)
              _RankRow(
                rank: i + 1,
                title: it['title'] as String? ?? '',
                subtitle: '${it['store']} · ${it['orders']} orders',
                trailing: '${it['units']} units',
                note: '₹${((it['revenue'] as num?) ?? 0).moneyText}',
                fraction: _share(it['units'], items.first['units']),
              ),
        ];

      case _Tab.products:
        final rows = _visible(
          _items
              .where(
                (i) =>
                    _search.isEmpty ||
                    '${i.title} ${i.storeName} ${i.category}'
                        .toLowerCase()
                        .contains(_search.toLowerCase()),
              )
              .map((i) => {'item': i})
              .toList(),
        );
        return [
          const _Note(
            'Every product in the shop, whichever store lists it. Stock is '
            'what is on the shelf; "in orders" is what is already spoken for, '
            'so a shopper sees the difference. A product with orders against '
            'it cannot be deleted — the orders are the record of those sales '
            '— so hide it from the shop instead.',
          ),
          if (_items.isEmpty)
            const _Empty('No products listed yet, by any store.')
          else if (rows.isEmpty)
            _Empty('No product matches "$_search".')
          else
            for (final row in rows)
              _ProductRow(
                item: row['item'] as InventoryItem,
                onDelete: () => _deleteItem(row['item'] as InventoryItem),
                onToggleListing: () =>
                    _toggleItemListing(row['item'] as InventoryItem),
                onEditStock: () => _editStock(row['item'] as InventoryItem),
              ),
        ];

      case _Tab.categories:
        final real = departments.where((d) => d.name != 'All').toList();
        final open = real.where((d) => d.name == _openDept).firstOrNull;

        // Drilling in rather than one long scroll. Expanded, this was 103
        // rows on one page — every department's whole menu at once, which is
        // not a view of anything.
        if (open != null) {
          return [
            Row(
              children: [
                IconButton(
                  onPressed: () => setState(() => _openDept = null),
                  icon: const Icon(LucideIcons.arrowLeft, size: 18),
                ),
                Expanded(
                  child: Text(
                    open.name,
                    style: const TextStyle(
                      fontSize: 17,
                      fontWeight: FontWeight.w800,
                    ),
                  ),
                ),
                TextButton.icon(
                  onPressed: () => _addCategory(parent: open.name),
                  icon: const Icon(LucideIcons.plus, size: 16),
                  label: const Text('Section'),
                ),
              ],
            ),
            const SizedBox(height: 4),
            if (open.categories.isEmpty)
              const _Empty(
                'Nothing in here yet. Sellers list under the department '
                'itself until you add a section.',
              )
            else
              for (final c in open.categories)
                _SectionCard(
                  node: c,
                  accent: open.colour ?? _ink,
                  onAdd: (parent) => _addCategory(parent: parent),
                  onDelete: _deleteCategory,
                  onPhoto: _setCategoryPhoto,
                ),
          ];
        }

        return [
          Row(
            children: [
              const Expanded(
                child: _Note(
                  'The tabs across the top of the shop. Open one to see what '
                  'sits under it — sellers pick from these, so adding one is '
                  'what makes it possible to sell in.',
                ),
              ),
              TextButton.icon(
                onPressed: () => _addCategory(),
                icon: const Icon(LucideIcons.plus, size: 16),
                label: const Text('Department'),
              ),
            ],
          ),
          if (real.isEmpty)
            const _Empty(
              'No departments. The shop has nothing but the All tab until '
              'you add one.',
            )
          else
            // Rows of two, not a Wrap. A Wrap sizes every child to its own
            // content, so a department that lists three categories stood 96px
            // tall beside one that lists none at 64px — the same row, two
            // heights, on every screen. A Row with stretch makes the pair
            // agree on the taller of the two.
            LayoutBuilder(
              builder: (context, box) {
                final perRow = box.maxWidth > 560 ? 2 : 1;
                Widget tile(Department d) => _DepartmentTile(
                  department: d,
                  onOpen: () => setState(() => _openDept = d.name),
                  onDelete: () => _deleteCategory(d.name),
                  onPhoto: () => _setCategoryPhoto(d.name),
                );
                return Column(
                  children: [
                    for (var start = 0; start < real.length; start += perRow)
                      Padding(
                        padding: EdgeInsets.only(
                          bottom: start + perRow < real.length ? 10 : 0,
                        ),
                        child: IntrinsicHeight(
                          child: Row(
                            crossAxisAlignment: CrossAxisAlignment.stretch,
                            children: [
                              for (
                                var i = start;
                                i < start + perRow && i < real.length;
                                i++
                              ) ...[
                                if (i > start) const SizedBox(width: 10),
                                Expanded(child: tile(real[i])),
                              ],
                              // Keeps a lone tile on the last row half-width
                              // rather than letting it stretch across.
                              if (real.length - start < perRow)
                                for (
                                  var pad = real.length - start;
                                  pad < perRow;
                                  pad++
                                ) ...[
                                  const SizedBox(width: 10),
                                  const Expanded(child: SizedBox.shrink()),
                                ],
                            ],
                          ),
                        ),
                      ),
                  ],
                );
              },
            ),
        ];

      case _Tab.policies:
        return [
          const _Note(
            'The written pages, as shoppers read them. Saving publishes '
            'straight away — there is no draft. A line starting with "## " '
            'is a heading; everything else is a paragraph.',
          ),
          if (_policies.isEmpty)
            const _Empty('No policies yet.')
          else
            for (final doc in _policies)
              _PolicyRow(doc: doc, onEdit: () => _editPolicy(doc)),
        ];

      case _Tab.compare:
        return [
          Row(
            children: [
              const Expanded(
                child: _Note(
                  'Categories say where to browse. These say what can be put '
                  'side by side — and the fields every product in the group '
                  'is asked for.',
                ),
              ),
              TextButton.icon(
                onPressed: () => _editGroup(),
                icon: const Icon(LucideIcons.plus, size: 16),
                label: const Text('Group'),
              ),
            ],
          ),
          if (_groups.isEmpty)
            const _Empty(
              'No comparison groups yet. Without one, Compare can only show '
              'the same product at different shops.',
            )
          else
            for (final g in _groups)
              _GroupCard(
                group: g,
                onEdit: () => _editGroup(g),
                onDelete: () => _deleteGroup(g.name),
              ),
        ];

      case _Tab.delivery:
        return [
          Row(
            children: [
              const Expanded(
                child: _Note(
                  'Riders sign in at /delivery with their number '
                  'and the PIN issued when you add them.',
                ),
              ),
              TextButton.icon(
                onPressed: _addRider,
                icon: const Icon(LucideIcons.plus, size: 16),
                label: const Text('Add number'),
              ),
            ],
          ),
          if (_riders.isEmpty)
            const _Empty('No delivery numbers yet.')
          else
            for (final r in _visible(_riders.cast<Map<String, dynamic>>()))
              _RiderRow(
                rider: r,
                onResetPin: () => _resetPin(r),
                onChangeNumber: () => _changeNumber(r),
                onSwitch: () => _switchRider(r),
                onDelete: () => _deleteRider(r),
              ),
        ];

      case _Tab.people:
        final people = (_overview?['people'] as List<dynamic>? ?? const [])
            .cast<Map<String, dynamic>>();
        return [
          const _Note(
            'Everyone who has signed in. Selling is not a setting — '
            'it follows from owning a store.',
          ),
          if (people.isEmpty)
            const _Empty('Nobody has signed in yet.')
          else
            for (final p in _visible(people.toList())) _PersonRow(person: p),
        ];
    }
  }
}

/// One comparison group: its name, how much is in it, and the fields.
class _GroupCard extends StatelessWidget {
  final CompareGroup group;
  final VoidCallback onEdit;
  final VoidCallback onDelete;
  const _GroupCard({
    required this.group,
    required this.onEdit,
    required this.onDelete,
  });

  @override
  Widget build(BuildContext context) {
    return Container(
      margin: const EdgeInsets.only(bottom: 10),
      padding: const EdgeInsets.fromLTRB(14, 12, 8, 14),
      decoration: BoxDecoration(
        color: Colors.white,
        borderRadius: BorderRadius.circular(16),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      group.name,
                      style: const TextStyle(
                        fontSize: 15,
                        fontWeight: FontWeight.w800,
                      ),
                    ),
                    Text(
                      group.attributes.isEmpty
                          ? '${group.items} products · no fields yet, so '
                                'there is nothing to compare on'
                          : '${group.items} products · '
                                '${group.attributes.length} fields',
                      style: const TextStyle(fontSize: 12, color: _muted),
                    ),
                  ],
                ),
              ),
              IconButton(
                tooltip: 'Edit fields',
                onPressed: onEdit,
                icon: const Icon(LucideIcons.pencil, size: 15),
              ),
              _QuietDelete(tooltip: 'Remove ${group.name}', onDelete: onDelete),
            ],
          ),
          if (group.attributes.isNotEmpty) ...[
            const SizedBox(height: 8),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                for (final a in group.attributes)
                  Container(
                    padding: const EdgeInsets.symmetric(
                      horizontal: 11,
                      vertical: 6,
                    ),
                    decoration: BoxDecoration(
                      color: LamazonTheme.canvas,
                      borderRadius: BorderRadius.circular(18),
                    ),
                    child: Text(
                      a.unit.isEmpty ? a.name : '${a.name} (${a.unit})',
                      style: const TextStyle(fontSize: 12.5),
                    ),
                  ),
              ],
            ),
          ],
        ],
      ),
    );
  }
}

/// A department a store may sell in. Unpicked is plain; picked shows its
/// place in the order, because the first one decides the shop's tab.
class _PickChip extends StatelessWidget {
  final String label;
  final int rank;
  final VoidCallback onTap;
  const _PickChip({
    required this.label,
    required this.rank,
    required this.onTap,
  });

  @override
  Widget build(BuildContext context) {
    final on = rank >= 0;
    return GestureDetector(
      onTap: onTap,
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 9),
        decoration: BoxDecoration(
          color: on ? _ink : Colors.transparent,
          borderRadius: BorderRadius.circular(20),
          border: Border.all(color: on ? _ink : const Color(0xFFDDDDD8)),
        ),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            if (on) ...[
              Container(
                width: 17,
                height: 17,
                alignment: Alignment.center,
                margin: const EdgeInsets.only(right: 7),
                decoration: const BoxDecoration(
                  color: Colors.white24,
                  shape: BoxShape.circle,
                ),
                child: Text(
                  '${rank + 1}',
                  style: const TextStyle(
                    fontSize: 10,
                    fontWeight: FontWeight.w800,
                    color: Colors.white,
                  ),
                ),
              ),
            ],
            Text(
              label,
              style: TextStyle(
                fontSize: 12.5,
                fontWeight: FontWeight.w700,
                color: on ? Colors.white : _ink,
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// What a department can be coloured. A fixed palette rather than a picker:
/// these sit behind the whole home screen, and an admin choosing freely is one
/// tap away from a tab nobody can read the text on.
const _palette = [
  '#2F6FED',
  '#43A047',
  '#FF8A3D',
  '#9C6ADE',
  '#F06292',
  '#00897B',
  '#5D4037',
  '#546E7A',
];

Color _hex(String value) => Color(
  0xFF000000 | (int.tryParse(value.replaceFirst('#', ''), radix: 16) ?? 0),
);

/// Everything under a department, at every level.
int _countAll(List<CategoryNode> nodes) =>
    nodes.fold(0, (n, c) => n + 1 + _countAll(c.children));

/// A department at a glance: what it is, how much is inside, and a taste of
/// it. The whole tree lives one tap in — a card that lists 69 things is not a
/// card.
class _DepartmentTile extends StatelessWidget {
  final Department department;
  final VoidCallback onOpen;
  final VoidCallback onDelete;
  final VoidCallback onPhoto;
  const _DepartmentTile({
    required this.department,
    required this.onOpen,
    required this.onDelete,
    required this.onPhoto,
  });

  @override
  Widget build(BuildContext context) {
    final accent = department.colour ?? _ink;
    final total = _countAll(department.categories);
    return InkWell(
      onTap: onOpen,
      borderRadius: BorderRadius.circular(16),
      child: Container(
        padding: const EdgeInsets.fromLTRB(14, 14, 8, 14),
        decoration: BoxDecoration(
          color: Colors.white,
          borderRadius: BorderRadius.circular(16),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                // The picture, and the way to change it: tapping the badge is
                // where an admin looks for it, so it needs no second button.
                _CategoryBadge(
                  imageUrl: department.imageUrl,
                  icon: department.icon,
                  accent: accent,
                  onTap: onPhoto,
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        department.name,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: const TextStyle(
                          fontSize: 15,
                          fontWeight: FontWeight.w800,
                        ),
                      ),
                      Text(
                        department.categories.isEmpty
                            ? 'Empty — sellers list under it directly'
                            : '${department.categories.length} sections · '
                                  '$total in total',
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: const TextStyle(fontSize: 12, color: _muted),
                      ),
                    ],
                  ),
                ),
                _QuietDelete(
                  tooltip: 'Remove ${department.name}',
                  onDelete: onDelete,
                ),
                const Icon(LucideIcons.chevronRight, size: 16, color: _muted),
              ],
            ),
            if (department.categories.isNotEmpty) ...[
              const SizedBox(height: 10),
              // The first few names, so a card says what is in there without
              // having to be opened.
              Text(
                department.categories.take(4).map((c) => c.name).join(' · ') +
                    (department.categories.length > 4
                        ? ' · +${department.categories.length - 4} more'
                        : ''),
                maxLines: 2,
                overflow: TextOverflow.ellipsis,
                style: const TextStyle(
                  fontSize: 12,
                  height: 1.4,
                  color: LamazonTheme.muted,
                ),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

/// The picture of a category, or its icon when it has none, with a tap to
/// change it. The camera corner is what says the square is a control.
class _CategoryBadge extends StatelessWidget {
  final String imageUrl;
  final IconData icon;
  final Color accent;
  final double size;
  final VoidCallback onTap;
  const _CategoryBadge({
    required this.imageUrl,
    required this.icon,
    required this.accent,
    required this.onTap,
    this.size = 36,
  });

  @override
  Widget build(BuildContext context) {
    return Tooltip(
      message: imageUrl.isEmpty ? 'Add a picture' : 'Replace the picture',
      child: InkWell(
        onTap: onTap,
        borderRadius: BorderRadius.circular(12),
        child: Stack(
          children: [
            Container(
              width: size,
              height: size,
              alignment: Alignment.center,
              clipBehavior: Clip.antiAlias,
              decoration: BoxDecoration(
                color: accent.withValues(alpha: 0.14),
                borderRadius: BorderRadius.circular(12),
              ),
              child: imageUrl.isEmpty
                  ? Icon(icon, size: size * 0.5, color: accent)
                  : NetImage(url: thumb(imageUrl, 120)),
            ),
            Positioned(
              right: 0,
              bottom: 0,
              child: Container(
                padding: const EdgeInsets.all(2),
                decoration: const BoxDecoration(
                  color: _ink,
                  shape: BoxShape.circle,
                ),
                child: const Icon(
                  LucideIcons.camera,
                  size: 8,
                  color: Colors.white,
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// One section inside a department, with what it holds folded away. Twelve
/// sections open at once was the wall; closed, the whole menu fits a screen.
class _SectionCard extends StatefulWidget {
  final CategoryNode node;
  final Color accent;
  final void Function(String parent) onAdd;
  final void Function(String name) onDelete;
  final void Function(String name) onPhoto;
  const _SectionCard({
    required this.node,
    required this.accent,
    required this.onAdd,
    required this.onDelete,
    required this.onPhoto,
  });

  @override
  State<_SectionCard> createState() => _SectionCardState();
}

class _SectionCardState extends State<_SectionCard> {
  bool _open = false;

  @override
  Widget build(BuildContext context) {
    final node = widget.node;
    return Container(
      margin: const EdgeInsets.only(bottom: 8),
      decoration: BoxDecoration(
        color: Colors.white,
        borderRadius: BorderRadius.circular(14),
      ),
      child: Column(
        children: [
          InkWell(
            onTap: node.children.isEmpty
                ? null
                : () => setState(() => _open = !_open),
            borderRadius: BorderRadius.circular(14),
            child: Padding(
              padding: const EdgeInsets.fromLTRB(14, 11, 6, 11),
              child: Row(
                children: [
                  Padding(
                    padding: const EdgeInsets.only(right: 10),
                    child: _CategoryBadge(
                      imageUrl: node.imageUrl,
                      icon: LucideIcons.image,
                      accent: widget.accent,
                      size: 30,
                      onTap: () => widget.onPhoto(node.name),
                    ),
                  ),
                  Expanded(
                    child: Text(
                      node.name,
                      style: const TextStyle(
                        fontSize: 14,
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                  ),
                  if (node.children.isNotEmpty)
                    Text(
                      '${node.children.length}',
                      style: const TextStyle(fontSize: 12, color: _muted),
                    ),
                  IconButton(
                    tooltip: 'Add inside ${node.name}',
                    onPressed: () => widget.onAdd(node.name),
                    icon: const Icon(LucideIcons.plus, size: 15),
                  ),
                  IconButton(
                    tooltip: 'Remove ${node.name}',
                    onPressed: () => widget.onDelete(node.name),
                    icon: const Icon(LucideIcons.x, size: 14, color: _muted),
                  ),
                  if (node.children.isNotEmpty)
                    Icon(
                      // const on both arms: a runtime LucideIcons read is
                      // what crashes the DDC debug build. See _FieldState.
                      _open ? _chevronUp : _chevronDown,
                      size: 15,
                      color: _muted,
                    ),
                ],
              ),
            ),
          ),
          if (_open)
            Padding(
              padding: const EdgeInsets.fromLTRB(28, 0, 10, 12),
              child: Wrap(
                spacing: 8,
                runSpacing: 8,
                children: [
                  for (final child in node.children)
                    Container(
                      padding: const EdgeInsets.fromLTRB(6, 6, 6, 6),
                      decoration: BoxDecoration(
                        color: LamazonTheme.canvas,
                        borderRadius: BorderRadius.circular(18),
                      ),
                      child: Row(
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          _CategoryBadge(
                            imageUrl: child.imageUrl,
                            icon: LucideIcons.image,
                            accent: widget.accent,
                            size: 22,
                            onTap: () => widget.onPhoto(child.name),
                          ),
                          const SizedBox(width: 8),
                          Text(
                            child.name,
                            style: const TextStyle(fontSize: 12.5),
                          ),
                          GestureDetector(
                            onTap: () => widget.onDelete(child.name),
                            child: const Padding(
                              padding: EdgeInsets.all(4),
                              child: Icon(
                                LucideIcons.x,
                                size: 12,
                                color: _muted,
                              ),
                            ),
                          ),
                        ],
                      ),
                    ),
                ],
              ),
            ),
        ],
      ),
    );
  }
}

/// Share of the leader, clamped. The top row is always a full bar, and a zero
/// leader (no orders at all) gives zero rather than a divide by zero.
double _share(Object? value, Object? top) {
  final v = (value as num?)?.toDouble() ?? 0;
  final t = (top as num?)?.toDouble() ?? 0;
  if (t <= 0) return 0;
  return (v / t).clamp(0, 1).toDouble();
}

class _InsightHeading extends StatelessWidget {
  final String text;
  const _InsightHeading(this.text);

  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.only(bottom: 10),
    child: Text(
      text,
      style: const TextStyle(fontSize: 15, fontWeight: FontWeight.w800),
    ),
  );
}

/// One headline number.
class _Stat extends StatelessWidget {
  final String label;
  final String value;
  final Color? color;
  const _Stat({required this.label, required this.value, this.color});

  @override
  Widget build(BuildContext context) => Expanded(
    child: Container(
      padding: const EdgeInsets.symmetric(vertical: 14, horizontal: 12),
      decoration: BoxDecoration(
        color: Colors.white,
        borderRadius: BorderRadius.circular(16),
      ),
      child: Column(
        children: [
          FittedBox(
            child: Text(
              value,
              style: TextStyle(
                fontSize: 20,
                fontWeight: FontWeight.w800,
                color: color ?? _ink,
              ),
            ),
          ),
          const SizedBox(height: 2),
          Text(
            label,
            textAlign: TextAlign.center,
            style: const TextStyle(fontSize: 11.5, color: _muted),
          ),
        ],
      ),
    ),
  );
}

/// A leaderboard line: position, what it is, and a bar the length of its
/// share of the leader. The bar is what makes a list of numbers a ranking you
/// can read without doing the arithmetic.
class _RankRow extends StatelessWidget {
  final int rank;
  final String title;
  final String subtitle;
  final String trailing;
  final String note;
  final double fraction;
  const _RankRow({
    required this.rank,
    required this.title,
    required this.subtitle,
    required this.trailing,
    required this.note,
    required this.fraction,
  });

  @override
  Widget build(BuildContext context) {
    return Container(
      margin: const EdgeInsets.only(bottom: 8),
      padding: const EdgeInsets.fromLTRB(14, 12, 14, 12),
      decoration: BoxDecoration(
        color: Colors.white,
        borderRadius: BorderRadius.circular(14),
      ),
      child: Column(
        children: [
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Container(
                width: 24,
                height: 24,
                alignment: Alignment.center,
                margin: const EdgeInsets.only(right: 10),
                decoration: BoxDecoration(
                  // The top three are the answer to "who is winning"; the
                  // rest are context.
                  color: rank <= 3 ? _ink : const Color(0xFFE8E8E4),
                  shape: BoxShape.circle,
                ),
                child: Text(
                  '$rank',
                  style: TextStyle(
                    fontSize: 11.5,
                    fontWeight: FontWeight.w800,
                    color: rank <= 3 ? Colors.white : _muted,
                  ),
                ),
              ),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      title,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: const TextStyle(
                        fontSize: 14,
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                    const SizedBox(height: 1),
                    Text(
                      subtitle,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: const TextStyle(fontSize: 12, color: _muted),
                    ),
                  ],
                ),
              ),
              const SizedBox(width: 8),
              Column(
                crossAxisAlignment: CrossAxisAlignment.end,
                children: [
                  Text(
                    trailing,
                    style: const TextStyle(
                      fontSize: 13.5,
                      fontWeight: FontWeight.w800,
                    ),
                  ),
                  Text(
                    note,
                    style: const TextStyle(fontSize: 11.5, color: _muted),
                  ),
                ],
              ),
            ],
          ),
          const SizedBox(height: 10),
          ClipRRect(
            borderRadius: BorderRadius.circular(999),
            child: LinearProgressIndicator(
              value: fraction,
              minHeight: 5,
              backgroundColor: const Color(0xFFEDEDE9),
              valueColor: AlwaysStoppedAnimation(
                rank == 1 ? _green : _ink.withValues(alpha: 0.55),
              ),
            ),
          ),
        ],
      ),
    );
  }
}

/// The sections of the panel, in the order they matter: what needs a decision
/// first, then what has been decided, then the day-to-day.
/// The three views of one list, as opposed to the eight destinations. They
/// read as filters because that is what they are.
const _storeTabs = [_Tab.review, _Tab.approved, _Tab.rejected];

/// One labelled row of chips. The caption sits beside them rather than over
/// them, so naming the two groups costs the panel no vertical space.
class _ChipGroup extends StatelessWidget {
  final String label;
  final String semanticLabel;
  final List<Widget> chips;
  const _ChipGroup({
    required this.label,
    required this.semanticLabel,
    required this.chips,
  });

  @override
  Widget build(BuildContext context) => Row(
    crossAxisAlignment: CrossAxisAlignment.start,
    children: [
      Padding(
        padding: const EdgeInsets.only(top: 10, right: 12),
        child: SizedBox(
          width: 62,
          child: Text(
            label.toUpperCase(),
            style: const TextStyle(
              fontSize: 11,
              fontWeight: FontWeight.w700,
              letterSpacing: .8,
              color: _muted,
            ),
          ),
        ),
      ),
      Expanded(
        child: Semantics(
          container: true,
          label: semanticLabel,
          child: Wrap(spacing: 8, runSpacing: 8, children: chips),
        ),
      ),
    ],
  );
}

enum _Tab {
  review('To review'),
  approved('Approved'),
  rejected('Rejected'),
  orders('Orders'),
  products('Products'),
  insights('Insights'),
  categories('Categories'),
  banners('Banners'),
  compare('Compare'),
  policies('Policies'),
  delivery('Delivery'),
  people('People');

  final String label;
  const _Tab(this.label);
}

/// One product, everywhere it is sold from, with the numbers that decide what
/// its buttons are allowed to do.
class _ProductRow extends StatelessWidget {
  final InventoryItem item;
  final VoidCallback onDelete;
  final VoidCallback onToggleListing;
  final VoidCallback onEditStock;
  const _ProductRow({
    required this.item,
    required this.onDelete,
    required this.onToggleListing,
    required this.onEditStock,
  });

  Color get _stockColour => switch (item.status) {
    StockStatus.inStock => _green,
    StockStatus.low => _amber,
    StockStatus.out => _red,
  };

  @override
  Widget build(BuildContext context) {
    final wide = isWide(context);
    return Padding(
      padding: const EdgeInsets.only(bottom: 10),
      child: ElevatedSurface(
        radius: LamazonTheme.featuredRadius,
        padding: const EdgeInsets.all(14),
        child: Flex(
          direction: wide ? Axis.horizontal : Axis.vertical,
          crossAxisAlignment: wide
              ? CrossAxisAlignment.center
              : CrossAxisAlignment.start,
          children: [
            Expanded(
              flex: wide ? 1 : 0,
              child: Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  ClipRRect(
                    borderRadius: BorderRadius.circular(
                      LamazonTheme.smallRadius,
                    ),
                    child: SizedBox(
                      width: 52,
                      height: 52,
                      child: item.imageUrls.isEmpty
                          ? Container(
                              color: LamazonTheme.track,
                              child: const Icon(
                                LucideIcons.imageOff,
                                size: 18,
                                color: _muted,
                              ),
                            )
                          : NetImage(
                              url: item.imageUrls.first,
                              sourceWidth: 160,
                            ),
                    ),
                  ),
                  const SizedBox(width: 12),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          item.title,
                          maxLines: 2,
                          overflow: TextOverflow.ellipsis,
                          style: const TextStyle(
                            fontSize: 14.5,
                            fontWeight: FontWeight.w700,
                          ),
                        ),
                        const SizedBox(height: 2),
                        Text(
                          '${item.storeName} · ${item.category.isEmpty ? "Uncategorised" : item.category}',
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: const TextStyle(fontSize: 12, color: _muted),
                        ),
                        const SizedBox(height: 6),
                        Wrap(
                          spacing: 6,
                          runSpacing: 6,
                          children: [
                            _Pill(
                              text: item.status.label,
                              colour: _stockColour,
                            ),
                            if (item.delisted)
                              const _Pill(text: 'Hidden', colour: _muted),
                            if (item.orders > 0)
                              _Pill(
                                text: item.orders == 1
                                    ? '1 order'
                                    : '${item.orders} orders',
                                colour: _muted,
                              ),
                            if (item.discounted)
                              _Pill(
                                text: '${item.discountPercent}% off',
                                colour: _green,
                              ),
                          ],
                        ),
                      ],
                    ),
                  ),
                ],
              ),
            ),
            SizedBox(width: wide ? 16 : 0, height: wide ? 0 : 12),
            // The three numbers that matter, and they do not mean the same
            // thing: stock is the shelf, reserved is already sold, and what a
            // shopper can buy is the difference.
            Expanded(
              flex: wide ? 1 : 0,
              child: Wrap(
                spacing: 18,
                runSpacing: 8,
                children: [
                  _Figure(label: 'Price', value: '₹${item.price.moneyText}'),
                  if (item.discounted)
                    _Figure(label: 'MRP', value: '₹${item.mrp.moneyText}'),
                  _Figure(label: 'On shelf', value: '${item.stock}'),
                  if (item.reserved > 0)
                    _Figure(label: 'In orders', value: '${item.reserved}'),
                  _Figure(
                    label: 'Can be sold',
                    value: '${item.available}',
                    colour: _stockColour,
                  ),
                  _Figure(
                    label: 'Shelf value',
                    value: '₹${item.value.moneyText}',
                  ),
                ],
              ),
            ),
            SizedBox(width: wide ? 8 : 0, height: wide ? 0 : 10),
            Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                IconButton(
                  tooltip: 'Correct the stock count',
                  onPressed: onEditStock,
                  icon: const Icon(LucideIcons.pencil, size: 16),
                ),
                IconButton(
                  tooltip: item.delisted
                      ? 'Put ${item.title} back on sale'
                      : 'Hide ${item.title} from the shop',
                  onPressed: onToggleListing,
                  icon: Icon(
                    item.delisted ? _eye : _eyeOff,
                    size: 16,
                    color: _muted,
                  ),
                ),
                _QuietDelete(
                  tooltip: 'Delete ${item.title}',
                  onDelete: onDelete,
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}

/// A small status word. Tinted, never colour alone — the word carries it.
class _Pill extends StatelessWidget {
  final String text;
  final Color colour;
  const _Pill({required this.text, required this.colour});

  @override
  Widget build(BuildContext context) => Container(
    padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
    decoration: BoxDecoration(
      color: colour.withValues(alpha: .12),
      borderRadius: BorderRadius.circular(10),
    ),
    child: Text(
      text,
      style: TextStyle(
        fontSize: 11,
        fontWeight: FontWeight.w700,
        color: colour,
      ),
    ),
  );
}

/// One labelled number. The label is not optional: "18" on its own says
/// nothing, and it is what the KPI tiles used to announce.
class _Figure extends StatelessWidget {
  final String label;
  final String value;
  final Color? colour;
  const _Figure({required this.label, required this.value, this.colour});

  @override
  Widget build(BuildContext context) => Semantics(
    label: '$label: $value',
    excludeSemantics: true,
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      mainAxisSize: MainAxisSize.min,
      children: [
        Text(label, style: const TextStyle(fontSize: 10.5, color: _muted)),
        const SizedBox(height: 1),
        Text(
          value,
          style: TextStyle(
            fontSize: 14,
            fontWeight: FontWeight.w700,
            color: colour ?? LamazonTheme.text,
          ),
        ),
      ],
    ),
  );
}

class _StoreCard extends StatelessWidget {
  final Map<String, dynamic> store;
  final VoidCallback? onApprove;
  final VoidCallback? onReject;
  final VoidCallback? onEditCategories;
  const _StoreCard({
    required this.store,
    this.onApprove,
    this.onReject,
    this.onEditCategories,
  });

  Color get _colour => switch (store['status']) {
    'approved' => _green,
    'rejected' => _red,
    _ => _amber,
  };

  @override
  Widget build(BuildContext context) {
    final categories = (store['categories'] as List<dynamic>? ?? const []).join(
      ', ',
    );
    return Container(
      margin: const EdgeInsets.only(bottom: 10),
      padding: const EdgeInsets.all(14),
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
                  store['name'] as String? ?? '',
                  style: const TextStyle(
                    fontSize: 15,
                    fontWeight: FontWeight.w800,
                  ),
                ),
              ),
              Container(
                padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
                decoration: BoxDecoration(
                  color: _colour.withValues(alpha: 0.12),
                  borderRadius: BorderRadius.circular(10),
                ),
                child: Text(
                  // Not the raw column value. "pending" is what the database
                  // calls it; an admin wants to know what it means for the
                  // shop.
                  switch (store['status']) {
                    'approved' => 'Live in the shop',
                    'rejected' => 'Rejected',
                    _ => 'Waiting for review',
                  },
                  style: TextStyle(
                    fontSize: 11,
                    fontWeight: FontWeight.w700,
                    color: _colour,
                  ),
                ),
              ),
            ],
          ),
          const SizedBox(height: 4),
          Text(
            // join, not interpolation: a store with no phone number rendered
            // as "owner@x.test · " with the separator left hanging.
            [
              store['owner'] as String? ?? '',
              store['phone'] as String? ?? '',
            ].where((v) => v.trim().isNotEmpty).join(' · '),
            style: const TextStyle(fontSize: 12, color: _muted),
          ),
          Text(
            '${store['location']}, ${store['city']} · $categories · '
            "${store['items']} ${store['items'] == 1 ? 'product' : 'products'}",
            style: const TextStyle(fontSize: 12, color: _muted),
          ),
          if ((store['rejectReason'] as String? ?? '').isNotEmpty)
            Padding(
              padding: const EdgeInsets.only(top: 4),
              child: Text(
                'Reason: ${store['rejectReason']}',
                style: const TextStyle(fontSize: 12, color: _red),
              ),
            ),
          const SizedBox(height: 12),
          // A Wrap, not a Row with a Spacer. Four controls and a Spacer needed
          // 629px and had 307 on a phone, so the store-approval queue — the
          // first screen an admin sees — overflowed by 322px.
          //
          // Splitting it also gives it the hierarchy the single row hid: the
          // decision this card exists for comes first, and the two upkeep
          // tools sit below it at lower weight rather than beside it at equal
          // weight with a Spacer pretending to separate them.
          if (onApprove != null || onReject != null) ...[
            Wrap(
              spacing: 10,
              runSpacing: 8,
              children: [
                if (onApprove != null)
                  FilledButton(
                    style: FilledButton.styleFrom(backgroundColor: _green),
                    onPressed: onApprove,
                    child: const Text('Approve'),
                  ),
                if (onReject != null)
                  OutlinedButton(
                    onPressed: onReject,
                    child: const Text('Reject', style: TextStyle(color: _red)),
                  ),
              ],
            ),
            const SizedBox(height: 4),
          ],
          Wrap(
            spacing: 4,
            runSpacing: 4,
            crossAxisAlignment: WrapCrossAlignment.center,
            children: [
              // Photos are the other thing an admin gets asked to fix, and
              // asking the seller to re-upload is a slower answer than doing
              // it.
              StorePhotosButton(
                owner: store['owner'] as String? ?? '',
                storeName: store['name'] as String? ?? '',
              ),
              // A shop that opened as Electronics and grew into Books had no
              // way to say so — the list was set once at onboarding.
              if (onEditCategories != null)
                TextButton.icon(
                  onPressed: onEditCategories,
                  icon: const Icon(LucideIcons.pencil, size: 15),
                  label: const Text('Departments'),
                ),
            ],
          ),
        ],
      ),
    );
  }
}

/// One order with its stage and whoever is carrying it. Assigning is only
/// offered while it can still change hands.
class _AdminOrderRow extends StatelessWidget {
  final Map<String, dynamic> order;
  final List<dynamic> riders;
  final VoidCallback onAssign;
  const _AdminOrderRow({
    required this.order,
    required this.riders,
    required this.onAssign,
  });

  Color get _colour => switch (order['stage']) {
    'delivered' => _green,
    'rejected' => _red,
    'picked' => const Color(0xFF6A1B9A),
    'accepted' => const Color(0xFF2F6FED),
    _ => _amber,
  };

  String _nameOf(String phone) {
    if (phone.isEmpty) return '';
    final match = riders
        .cast<Map<String, dynamic>>()
        .where((r) => r['phone'] == phone)
        .firstOrNull;
    final name = match?['name'] as String? ?? '';
    return name.isEmpty ? phone : '$name · $phone';
  }

  @override
  Widget build(BuildContext context) {
    final stage = order['stage'] as String;
    final carrier = order['riderPhone'] as String? ?? '';
    final assigned = order['assignedTo'] as String? ?? '';
    final canAssign = stage == 'received' || stage == 'accepted';
    return Container(
      margin: const EdgeInsets.only(bottom: 8),
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: Colors.white,
        borderRadius: BorderRadius.circular(16),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Expanded(
                child: Text(
                  '#${(order['id'] as String).toUpperCase()} · '
                  '${order['units']} × ${order['itemTitle']}',
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: const TextStyle(
                    fontSize: 13.5,
                    fontWeight: FontWeight.w700,
                  ),
                ),
              ),
              Container(
                padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
                decoration: BoxDecoration(
                  color: _colour.withValues(alpha: 0.12),
                  borderRadius: BorderRadius.circular(10),
                ),
                child: Text(
                  stage,
                  style: TextStyle(
                    fontSize: 11,
                    fontWeight: FontWeight.w700,
                    color: _colour,
                  ),
                ),
              ),
            ],
          ),
          const SizedBox(height: 3),
          Text(
            '₹${((order['amount'] as num?) ?? 0).moneyText} · '
            '${order['storeName']} → ${order['receiverName']} · '
            '${order['receiverPhone']}',
            style: const TextStyle(fontSize: 12, color: _muted),
          ),
          Text(
            '${order['receiverAddress']}',
            style: const TextStyle(fontSize: 11.5, color: _muted),
          ),
          const SizedBox(height: 6),
          Row(
            children: [
              Expanded(
                child: Text(
                  carrier.isNotEmpty
                      ? 'Carried by ${_nameOf(carrier)}'
                      : assigned.isNotEmpty
                      ? 'Assigned to ${_nameOf(assigned)}'
                      : 'Open to any rider',
                  style: TextStyle(
                    fontSize: 12,
                    fontWeight: FontWeight.w600,
                    color: assigned.isEmpty && carrier.isEmpty ? _muted : _ink,
                  ),
                ),
              ),
              if (canAssign)
                TextButton(
                  onPressed: onAssign,
                  child: Text(assigned.isEmpty ? 'Assign rider' : 'Reassign'),
                ),
            ],
          ),
        ],
      ),
    );
  }
}

class _RiderRow extends StatelessWidget {
  final Map<String, dynamic> rider;
  final VoidCallback onResetPin;
  final VoidCallback onChangeNumber;
  final VoidCallback onSwitch;
  final VoidCallback onDelete;
  const _RiderRow({
    required this.rider,
    required this.onResetPin,
    required this.onChangeNumber,
    required this.onSwitch,
    required this.onDelete,
  });

  @override
  Widget build(BuildContext context) {
    final active = rider['active'] == true;
    return Container(
      margin: const EdgeInsets.only(bottom: 8),
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: Colors.white,
        borderRadius: BorderRadius.circular(16),
      ),
      child: Row(
        children: [
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  '${rider['name']?.toString().isEmpty ?? true ? 'Rider' : rider['name']} · ${rider['phone']}',
                  style: const TextStyle(
                    fontSize: 13.5,
                    fontWeight: FontWeight.w700,
                  ),
                ),
                Text(
                  '${rider['delivered']} delivered · ${rider['carrying']} carrying',
                  style: const TextStyle(fontSize: 12, color: _muted),
                ),
                // What "off" means, where it is read, rather than one word
                // that could mean anything.
                if (!active)
                  const Text(
                    'Switched off — not signed in, and not being handed orders',
                    style: TextStyle(fontSize: 11.5, color: _amber),
                  ),
              ],
            ),
          ),
          PopupMenuButton<String>(
            icon: const Icon(LucideIcons.ellipsisVertical, size: 18),
            onSelected: (choice) => switch (choice) {
              'pin' => onResetPin(),
              'number' => onChangeNumber(),
              'switch' => onSwitch(),
              _ => onDelete(),
            },
            itemBuilder: (_) => [
              const PopupMenuItem(value: 'pin', child: Text('New PIN')),
              const PopupMenuItem(
                value: 'number',
                child: Text('Change number'),
              ),
              PopupMenuItem(
                value: 'switch',
                child: Text(active ? 'Switch off' : 'Switch back on'),
              ),
              const PopupMenuItem(
                value: 'delete',
                child: Text('Remove for good', style: TextStyle(color: _red)),
              ),
            ],
          ),
        ],
      ),
    );
  }
}

class _PersonRow extends StatelessWidget {
  final Map<String, dynamic> person;
  const _PersonRow({required this.person});

  @override
  Widget build(BuildContext context) {
    final store = person['storeName'] as String? ?? '';
    final seller = store.isNotEmpty;
    return Container(
      margin: const EdgeInsets.only(bottom: 8),
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: Colors.white,
        borderRadius: BorderRadius.circular(16),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          // The badge sits on the same line as the address but cannot be
          // pushed into it: a long address wraps inside its own half.
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Expanded(
                child: Text(
                  person['email'] as String? ?? '',
                  style: const TextStyle(
                    fontSize: 13,
                    fontWeight: FontWeight.w600,
                  ),
                ),
              ),
              const SizedBox(width: 8),
              Container(
                padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
                decoration: BoxDecoration(
                  color: (seller ? _green : _muted).withValues(alpha: 0.12),
                  borderRadius: BorderRadius.circular(10),
                ),
                child: Text(
                  seller ? 'Buyer + Seller' : 'Buyer',
                  style: TextStyle(
                    fontSize: 10.5,
                    fontWeight: FontWeight.w700,
                    color: seller ? _green : _muted,
                  ),
                ),
              ),
            ],
          ),
          const SizedBox(height: 3),
          Text(
            [
              person['id'],
              if ((person['name'] as String? ?? '').isNotEmpty) person['name'],
              if ((person['phone'] as String? ?? '').isNotEmpty)
                person['phone'],
              if (seller) '$store (${person['storeStatus']})',
            ].join(' · '),
            style: const TextStyle(fontSize: 11.5, color: _muted),
          ),
        ],
      ),
    );
  }
}

class _Tile extends StatelessWidget {
  final String label;
  final String value;
  final Color? color;
  final VoidCallback? onTap;
  const _Tile({
    required this.label,
    required this.value,
    this.color,
    this.onTap,
  });

  @override
  Widget build(BuildContext context) {
    return Expanded(
      // One node saying "Orders 4", not two saying "4" and "Orders" — and
      // definitely not the six bare digits these used to announce, which
      // told a screen-reader user nothing at all.
      //
      // The label goes on the surface rather than in a Semantics around it:
      // a second annotation outside made two nodes, one carrying the name and
      // the other carrying the tab stop, so the tile a keyboard could reach
      // was the one with nothing to say.
      child: ElevatedSurface(
        onTap: onTap,
        semanticLabel: '$label: $value',
        radius: LamazonTheme.featuredRadius,
        padding: const EdgeInsets.symmetric(vertical: 16, horizontal: 12),
        child: Column(
          children: [
            Text(
              value,
              style: TextStyle(
                fontSize: 22,
                fontWeight: FontWeight.w800,
                color: color ?? LamazonTheme.text,
              ),
            ),
            const SizedBox(height: 2),
            Text(
              label,
              textAlign: TextAlign.center,
              style: const TextStyle(fontSize: 12, color: LamazonTheme.muted),
            ),
          ],
        ),
      ),
    );
  }
}

class _Note extends StatelessWidget {
  final String text;
  const _Note(this.text);

  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.only(bottom: 10),
    child: Text(
      text,
      style: const TextStyle(fontSize: 12, height: 1.4, color: _muted),
    ),
  );
}

/// Nothing here, said once and properly.
///
/// This was a bare grey sentence sitting under an explanatory note and above
/// a "0 records" pagination row — three stacked messages for one empty list,
/// two of them contradicting each other in tone. The note above still
/// explains what the section is for; this says the list is empty and stops.
///
/// No call to action: most of these have no next step the admin could take
/// from here, and inventing one would be worse than the silence.
class _Empty extends StatelessWidget {
  final String text;
  const _Empty(this.text);

  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.only(bottom: 12),
    child: ElevatedSurface(
      radius: LamazonTheme.featuredRadius,
      padding: const EdgeInsets.symmetric(vertical: 28, horizontal: 20),
      child: Row(
        mainAxisAlignment: MainAxisAlignment.center,
        children: [
          Container(
            width: 34,
            height: 34,
            decoration: BoxDecoration(
              color: LamazonTheme.track,
              borderRadius: BorderRadius.circular(LamazonTheme.smallRadius),
            ),
            child: const Icon(
              LucideIcons.inbox,
              size: 17,
              color: LamazonTheme.muted,
            ),
          ),
          const SizedBox(width: 12),
          Flexible(
            child: Text(
              text,
              style: const TextStyle(fontSize: 13.5, color: _muted),
            ),
          ),
        ],
      ),
    ),
  );
}

class _Field extends StatefulWidget {
  final TextEditingController controller;
  final String label;
  final bool obscure;
  final VoidCallback? onSubmit;
  final Iterable<String>? autofill;
  const _Field({
    required this.controller,
    required this.label,
    this.obscure = false,
    this.onSubmit,
    this.autofill,
  });

  @override
  State<_Field> createState() => _FieldState();
}

class _FieldState extends State<_Field> {
  bool _shown = false;

  @override
  Widget build(BuildContext context) {
    return TextField(
      controller: widget.controller,
      obscureText: widget.obscure && !_shown,
      autofillHints: widget.autofill,
      onSubmitted: widget.onSubmit == null ? null : (_) => widget.onSubmit!(),
      decoration: InputDecoration(
        labelText: widget.label,
        // The card behind this is already `surface`; white on it left the
        // field with almost no edge, so it did not read as somewhere to type.
        fillColor: LamazonTheme.canvas,
        // A staff password typed blind into a panel that answers "wrong
        // username or password" and nothing more is a slow way to find a typo.
        suffixIcon: !widget.obscure
            ? null
            : Semantics(
                button: true,
                label: _shown ? 'Hide password' : 'Show password',
                child: Tooltip(
                  message: _shown ? 'Hide password' : 'Show password',
                  excludeFromSemantics: true,
                  child: IconButton(
                    onPressed: () => setState(() => _shown = !_shown),
                    // Two const Icons rather than one Icon holding a
                    // conditional. That looks like a style choice and is not:
                    // `_shown ? LucideIcons.eyeOff : LucideIcons.eye` reads
                    // those statics at runtime, and LucideIcons is one class
                    // with 27,874 static consts whose lazy initialisation
                    // exhausts the stack in a DDC debug build. The whole
                    // sign-in card rendered as a red "Stack Overflow" box, so
                    // the admin panel could not be opened by anyone
                    // developing it. Written as const the values are folded at
                    // compile time and the initialiser never runs — which is
                    // why the shield above, inside a const subtree, always
                    // rendered fine while this did not.
                    //
                    // Release builds compile with dart2js and were never
                    // affected. admin_login_test.dart pins the card.
                    icon: _shown
                        ? const Icon(
                            LucideIcons.eyeOff,
                            size: 18,
                            color: LamazonTheme.muted,
                          )
                        : const Icon(
                            LucideIcons.eye,
                            size: 18,
                            color: LamazonTheme.muted,
                          ),
                  ),
                ),
              ),
      ),
    );
  }
}

/// One policy in the admin list: what it is, and how long it is.
class _PolicyRow extends StatelessWidget {
  final PolicyDoc doc;
  final VoidCallback onEdit;
  const _PolicyRow({required this.doc, required this.onEdit});

  @override
  Widget build(BuildContext context) {
    final words = doc.body.trim().split(RegExp(r'\s+')).length;
    return Container(
      margin: const EdgeInsets.only(bottom: 8),
      padding: const EdgeInsets.fromLTRB(14, 12, 8, 12),
      decoration: BoxDecoration(
        color: Colors.white,
        borderRadius: BorderRadius.circular(14),
      ),
      child: Row(
        children: [
          Icon(doc.icon, size: 18, color: _ink),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  doc.title,
                  style: const TextStyle(
                    fontSize: 14,
                    fontWeight: FontWeight.w700,
                  ),
                ),
                Text(
                  // The blanks are the point: an admin should be able to see
                  // at a glance which pages still say [Company Name].
                  '$words words'
                  '${doc.body.contains('[') ? ' · has blanks to fill in' : ''}',
                  style: TextStyle(
                    fontSize: 12,
                    color: doc.body.contains('[') ? _amber : _muted,
                  ),
                ),
              ],
            ),
          ),
          TextButton.icon(
            onPressed: onEdit,
            icon: const Icon(LucideIcons.pencil, size: 15),
            label: const Text('Edit'),
            style: TextButton.styleFrom(foregroundColor: _ink),
          ),
        ],
      ),
    );
  }
}

/// The whole document in one box, with a preview of how it will read.
class _PolicyEditor extends StatefulWidget {
  final PolicyDoc doc;
  const _PolicyEditor({required this.doc});

  @override
  State<_PolicyEditor> createState() => _PolicyEditorState();
}

class _PolicyEditorState extends State<_PolicyEditor> {
  late final _title = TextEditingController(text: widget.doc.title);
  late final _body = TextEditingController(text: widget.doc.body);
  bool _preview = false;
  bool _busy = false;
  String? _error;

  @override
  void dispose() {
    _title.dispose();
    _body.dispose();
    super.dispose();
  }

  Future<void> _save() async {
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await Api.instance.savePolicy(
        widget.doc.slug,
        _title.text.trim(),
        _body.text,
      );
      if (mounted) Navigator.pop(context, true);
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _error = e.toString().replaceFirst('ClientException: ', '');
        _busy = false;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: LamazonTheme.canvas,
      body: ReadableBody(
        maxWidth: 760,
        child: SafeArea(
          child: Column(
            children: [
              ScreenHeader(
                title: widget.doc.title,
                action: IconButton(
                  tooltip: _preview ? 'Back to editing' : 'Preview',
                  onPressed: () => setState(() => _preview = !_preview),
                  icon: Icon(_preview ? _pencil : _eye, size: 17, color: _ink),
                ),
              ),
              Expanded(
                child: ListView(
                  padding: const EdgeInsets.fromLTRB(20, 4, 20, 24),
                  children: [
                    if (_preview)
                      // Exactly the widget the shopper sees, so the preview
                      // cannot disagree with the page.
                      PolicyBody(text: _body.text)
                    else ...[
                      const Text(
                        'Title',
                        style: TextStyle(
                          fontSize: 12.5,
                          fontWeight: FontWeight.w700,
                          color: _muted,
                        ),
                      ),
                      const SizedBox(height: 8),
                      TextField(
                        controller: _title,
                        decoration: _boxed(),
                        style: const TextStyle(
                          fontSize: 15,
                          fontWeight: FontWeight.w700,
                        ),
                      ),
                      const SizedBox(height: 18),
                      const Text(
                        'Text',
                        style: TextStyle(
                          fontSize: 12.5,
                          fontWeight: FontWeight.w700,
                          color: _muted,
                        ),
                      ),
                      const SizedBox(height: 8),
                      TextField(
                        controller: _body,
                        maxLines: null,
                        minLines: 18,
                        keyboardType: TextInputType.multiline,
                        onChanged: (_) => setState(() {}),
                        decoration: _boxed(),
                        style: const TextStyle(
                          fontSize: 13,
                          height: 1.5,
                          fontFamily: 'monospace',
                        ),
                      ),
                    ],
                    if (_error != null) ...[
                      const SizedBox(height: 12),
                      Text(
                        _error!,
                        style: const TextStyle(fontSize: 12.5, color: _red),
                      ),
                    ],
                  ],
                ),
              ),
              Padding(
                padding: const EdgeInsets.fromLTRB(20, 4, 20, 16),
                child: SizedBox(
                  width: double.infinity,
                  child: FilledButton(
                    style: FilledButton.styleFrom(
                      backgroundColor: _ink,
                      padding: const EdgeInsets.symmetric(vertical: 16),
                      shape: RoundedRectangleBorder(
                        borderRadius: BorderRadius.circular(26),
                      ),
                    ),
                    onPressed: _busy || _body.text.trim().isEmpty
                        ? null
                        : _save,
                    child: Text(
                      _busy ? 'Publishing…' : 'Publish',
                      style: const TextStyle(fontWeight: FontWeight.w700),
                    ),
                  ),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }

  InputDecoration _boxed() => InputDecoration(
    filled: true,
    fillColor: Colors.white,
    contentPadding: const EdgeInsets.all(14),
    border: OutlineInputBorder(
      borderRadius: BorderRadius.circular(14),
      borderSide: BorderSide.none,
    ),
  );
}

/// A delete that waits to be looked for.
///
/// It reads as muted until pointed at or focused, then turns red. The action
/// is exactly as available as before — same tooltip, same target size, same
/// place — but a list of eight of them no longer outshouts the one primary
/// button on the screen.
class _QuietDelete extends StatefulWidget {
  final String tooltip;
  final VoidCallback? onDelete;
  const _QuietDelete({required this.tooltip, required this.onDelete});

  @override
  State<_QuietDelete> createState() => _QuietDeleteState();
}

class _QuietDeleteState extends State<_QuietDelete> {
  bool _lit = false;

  @override
  Widget build(BuildContext context) => Focus(
    canRequestFocus: false,
    skipTraversal: true,
    onFocusChange: (has) => setState(() => _lit = has),
    child: IconButton(
      tooltip: widget.tooltip,
      onPressed: widget.onDelete,
      onHover: (over) => setState(() => _lit = over),
      icon: Icon(LucideIcons.trash2, size: 15, color: _lit ? _red : _muted),
    ),
  );
}
