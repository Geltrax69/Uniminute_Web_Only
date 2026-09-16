import 'dart:async';

import 'package:flutter/material.dart';
import 'package:lucide_icons_flutter/lucide_icons.dart';

import '../data/addresses.dart';
import '../data/api.dart';
import '../data/campaigns.dart';
import '../data/catalog.dart';
import '../data/categories.dart';
import '../data/orders.dart';
import '../data/season.dart';
import '../data/session.dart';
import '../data/wishlist.dart';
import '../models/product.dart';
import '../widgets/app_nav.dart';
import '../widgets/app_shell.dart';
import '../widgets/campaign_palette.dart';
import '../widgets/category_visual.dart';
import '../widgets/design_system.dart';
import '../widgets/notify_banner.dart';
import '../widgets/product_card.dart';
import '../widgets/status_views.dart';
import '../widgets/storefront.dart';
import 'addresses_screen.dart';
import 'details_screen.dart';
import 'search_screen.dart';
import 'seller_dashboard_screen.dart';
import 'shop_screen.dart';
import 'shops_screen.dart';
import 'wishlist_screen.dart';

const _productPage = 12;

class HomeScreen extends StatefulWidget {
  const HomeScreen({super.key});

  @override
  State<HomeScreen> createState() => _HomeScreenState();
}

class _HomeScreenState extends State<HomeScreen> {
  late Future<(List<Product>, List<Shop>)> _future = _loadWithFallback();
  final _scroll = ScrollController();
  List<Campaign> _campaigns = const [];
  int _tab = 0;
  int _shownCount = _productPage;

  @override
  void dispose() {
    _scroll.dispose();
    super.dispose();
  }

  Future<List<Campaign>> _loadCampaigns() async {
    try {
      return await Api.instance.campaigns().timeout(
        const Duration(milliseconds: 1200),
      );
    } catch (error) {
      logApiFailure('campaigns', error);
      return [starterCampaign];
    }
  }

  Future<(List<Product>, List<Shop>)> _load() async {
    await loadDepartments().timeout(
      const Duration(milliseconds: 800),
      onTimeout: () => departments,
    );
    final (items, liveShops, campaigns) = await (
      loadCatalog().timeout(
        const Duration(milliseconds: 900),
        onTimeout: () => products,
      ),
      loadShops().timeout(
        const Duration(milliseconds: 900),
        onTimeout: () => shops,
      ),
      _loadCampaigns(),
    ).wait;
    _campaigns = campaigns;
    if (_tab >= departments.length) _tab = 0;
    return (items, liveShops);
  }

  Future<(List<Product>, List<Shop>)> _loadWithFallback() {
    return _load().timeout(
      const Duration(seconds: 2),
      onTimeout: () {
        _campaigns = [starterCampaign];
        return (products, shops);
      },
    );
  }

  void _selectDepartment(int index) {
    setState(() {
      _tab = index;
      _shownCount = _productPage;
    });
  }

  void _openCategory(String category, String department) {
    Navigator.push(
      context,
      MaterialPageRoute(
        builder: (_) => SearchScreen(initialQuery: category, tab: department),
      ),
    );
  }

  void _openSearch([String initialQuery = '']) {
    final department = departments[_tab].name;
    Navigator.push(
      context,
      MaterialPageRoute(
        builder: (_) => SearchScreen(
          initialQuery: initialQuery,
          tab: initialQuery.isEmpty ? department : departmentOf(initialQuery),
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: LamazonTheme.canvas,
      body: SafeArea(
        bottom: false,
        child: Stack(
          children: [
            FutureBuilder<(List<Product>, List<Shop>)>(
              future: _future,
              builder: (context, snapshot) {
                if (snapshot.connectionState != ConnectionState.done) {
                  return const CatalogSkeleton();
                }
                if (snapshot.hasError) {
                  return ErrorView(
                    onRetry: () =>
                        setState(() => _future = _loadWithFallback()),
                  );
                }
                final (items, liveShops) = snapshot.data!;
                return _content(items, liveShops);
              },
            ),
            const Align(
              alignment: Alignment.bottomCenter,
              child: AppBottomNav(),
            ),
          ],
        ),
      ),
    );
  }

  Widget _content(List<Product> items, List<Shop> liveShops) {
    final activeDepartment = departments[_tab];
    final tabName = activeDepartment.name;
    final scopedProducts = items
        .where((product) => _tab == 0 || product.tab == tabName)
        .toList();
    final scopedShops = liveShops
        .where((shop) => _tab == 0 || shop.tab == tabName)
        .toList();
    final offers = scopedProducts
        .where((product) => product.discounted && product.availableStock != 0)
        .take(10)
        .toList();
    final campaigns = _campaigns
        .where(
          (campaign) =>
              campaign.enabled &&
              (_tab == 0 ||
                  campaign.department == tabName ||
                  campaign.category == tabName),
        )
        .toList();
    final visible = scopedProducts.take(_shownCount).toList();
    // Hoisted: one answer for the whole grid, not one per card.
    final namesStores = mixesStores(visible);

    return RefreshIndicator(
      onRefresh: () async {
        final next = _load();
        setState(() => _future = next);
        await next;
      },
      child: LayoutBuilder(
        builder: (context, constraints) {
          final maxWidth = constraints.maxWidth >= 1500
              ? 1400.0
              : constraints.maxWidth;
          final side =
              (constraints.maxWidth - maxWidth) / 2 +
              LamazonTheme.gutter(context);
          return ListView(
            controller: _scroll,
            padding: EdgeInsets.fromLTRB(
              side,
              8,
              side,
              bottomNavInset(context) + 24,
            ),
            children: [
              const _ServiceHeader(),
              const SizedBox(height: 12),
              _SearchLaunch(
                onTap: () => Navigator.push(
                  context,
                  MaterialPageRoute(
                    builder: (_) =>
                        SearchScreen(tab: tabName == 'All' ? '' : tabName),
                  ),
                ),
              ),
              const SizedBox(height: 16),
              _DepartmentStrip(active: _tab, onSelect: _selectDepartment),
              const SizedBox(height: 22),
              if (campaigns.isNotEmpty)
                CampaignDeck(key: ValueKey(tabName), campaigns: campaigns)
              else
                CampaignBanner(
                  campaign: Campaign(
                    id: 'department-$tabName',
                    title: _departmentHeadline(tabName),
                    subtitle: 'A considered edit from stores in your area.',
                    category: tabName == 'All' ? '' : tabName,
                    colour: SeasonSkin.groundHex,
                    cta: 'Explore the edit',
                  ),
                  onTap: () => _openSearch(tabName == 'All' ? '' : tabName),
                ),
              // Below the hero, and only for somebody who has actually
              // ordered something. This used to sit above the campaign on
              // first paint, asking for a browser permission before the
              // person had done anything — which is the reliable way to get
              // it denied, permanently, before it could ever be useful.
              // Someone with a live order has something to be notified about.
              if (Session.instance.loggedIn &&
                  MyOrders.instance.orders.isNotEmpty) ...[
                const SizedBox(height: 22),
                const NotifyBanner(),
              ],
              if (offers.isNotEmpty) ...[
                const SizedBox(height: 34),
                CollectionShelf(
                  title: 'Around you',
                  subtitle: 'Fresh picks with a real saving',
                  products: offers,
                  onSeeAll: () => _openSearch(),
                ),
              ],
              // On All, one titled grid per department — the whole shop laid
              // out to scan, rather than a single board that made you choose a
              // department before it would show you anything. Scoped, just the
              // one department's shelves.
              if (_tab == 0)
                for (final (index, department) in departments.indexed)
                  if (index != 0 && department.categories.isNotEmpty) ...[
                    const SizedBox(height: 34),
                    _CategorySection(
                      title: department.name,
                      department: department.name,
                      categories: department.categories,
                      limit: 6,
                      onSeeAll: () => _selectDepartment(index),
                      onOpenCategory: _openCategory,
                    ),
                  ] else if (activeDepartment.categories.isNotEmpty) ...[
                    const SizedBox(height: 34),
                    _CategorySection(
                      title: 'Browse by category',
                      subtitle: 'Narrow $tabName down to one shelf',
                      department: tabName,
                      categories: activeDepartment.categories,
                      onOpenCategory: _openCategory,
                    ),
                  ],
              if (scopedShops.isNotEmpty) ...[
                const SizedBox(height: 36),
                SectionHeading(
                  title: 'Stores near you',
                  subtitle: 'Local storefronts, one tap away',
                  onAction: () => Navigator.push(
                    context,
                    MaterialPageRoute(
                      builder: (_) => ShopsScreen(tab: tabName),
                    ),
                  ),
                  actionLabel: 'See all',
                ),
                const SizedBox(height: 14),
                _StoreRail(shops: scopedShops),
              ],
              ListenableBuilder(
                listenable: Wishlist.instance,
                builder: (context, _) {
                  final saved = scopedProducts
                      .where(
                        (product) => Wishlist.instance.contains(product.id),
                      )
                      .take(10)
                      .toList();
                  if (saved.isEmpty) return const SizedBox.shrink();
                  return Padding(
                    padding: const EdgeInsets.only(top: 36),
                    child: CollectionShelf(
                      title: 'Saved for later',
                      subtitle: 'Your shortlist is waiting',
                      products: saved,
                      onSeeAll: () => Navigator.push(
                        context,
                        MaterialPageRoute(
                          builder: (_) => const WishlistScreen(),
                        ),
                      ),
                    ),
                  );
                },
              ),
              const SizedBox(height: 36),
              if (scopedProducts.isEmpty)
                _NothingHere(tab: tabName)
              else ...[
                SectionHeading(
                  title: _tab == 0
                      ? 'Discover local favourites'
                      : 'Explore $tabName',
                  subtitle: _tab == 0
                      ? 'Real products from nearby shops'
                      : 'Everything in this department',
                  onAction: () => _openSearch(),
                  actionLabel: 'See all',
                ),
                const SizedBox(height: 14),
                GridView.builder(
                  shrinkWrap: true,
                  padding: EdgeInsets.zero,
                  physics: const NeverScrollableScrollPhysics(),
                  gridDelegate: const SliverGridDelegateWithMaxCrossAxisExtent(
                    // productTileMax, like every other product grid. Home was
                    // the one screen with its own number, so the same card
                    // measured 232px here and 193px in search.
                    maxCrossAxisExtent: productTileMax,
                    mainAxisSpacing: 14,
                    crossAxisSpacing: 14,
                    childAspectRatio: productTileAspect,
                  ),
                  itemCount: visible.length,
                  itemBuilder: (_, index) => ProductCard(
                    product: visible[index],
                    showStore: namesStores,
                    showAddToCart:
                        visible[index].options.isEmpty &&
                        visible[index].sizes.isEmpty,
                    onTap: () => Navigator.push(
                      context,
                      MaterialPageRoute(
                        builder: (_) => DetailsScreen(product: visible[index]),
                      ),
                    ),
                  ),
                ),
                if (scopedProducts.length > visible.length) ...[
                  const SizedBox(height: 20),
                  Center(
                    child: ActionButton(
                      label:
                          'Show ${scopedProducts.length - visible.length} more',
                      onPressed: () =>
                          setState(() => _shownCount += _productPage),
                      icon: Icons.arrow_downward,
                      primary: false,
                    ),
                  ),
                ],
              ],
              const SizedBox(height: 42),
              const _HomeClose(),
            ],
          );
        },
      ),
    );
  }
}

String _departmentHeadline(String name) {
  return switch (CategoryVisual.indexFor(name)) {
    0 => 'A little delight, close by.',
    1 => 'A fresher kind of everyday.',
    2 => 'Small upgrades, beautifully useful.',
    3 => 'Something thoughtful, waiting nearby.',
    4 => 'Your everyday feel-good edit.',
    5 => 'Make home feel more like home.',
    6 => 'A little play goes a long way.',
    _ => 'Snack, sip, repeat.',
  };
}

class _ServiceHeader extends StatelessWidget {
  const _ServiceHeader();

  @override
  Widget build(BuildContext context) {
    // The chrome a festival repaints — shared with the hero, the department
    // tiles and the navigation bar, so a season dresses the whole shop rather
    // than bolting a navy header onto a forest page. Product cards, prices and
    // stock colours are deliberately not on this list.
    final ground = SeasonSkin.ground;
    final ink = SeasonSkin.ink;
    return DecoratedBox(
      decoration: BoxDecoration(
        borderRadius: BorderRadius.circular(LamazonTheme.featuredRadius),
        gradient: LinearGradient(
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
          // A season names one ground; the second stop is derived so a
          // festival palette does not have to specify a gradient to get one.
          colors: SeasonSkin.active
              ? [ground, SeasonSkin.groundShade]
              : const [LamazonTheme.forest, LamazonTheme.strong],
        ),
        boxShadow: LamazonTheme.raisedShadows,
      ),
      child: Padding(
        padding: const EdgeInsets.fromLTRB(16, 16, 16, 15),
        // Two controls came out of this header, both of them duplicates.
        //
        // The hamburger opened a drawer listing departments and their
        // categories — the fourth way to reach a department on this one screen,
        // after the strip 60px below it, the titled grid per department further
        // down, and "Shop by department" on search. It added nothing that was
        // not already on screen.
        //
        // The avatar on the right went to AppRoutes.account. So does the
        // Account tab in the bottom bar, which is always visible and carries a
        // label. The same destination twice, 40px apart vertically.
        //
        // What is left is what the header is actually for: who you are shopping
        // with, and where it is going. The location now gets the full width,
        // which is why a long address no longer ellipses.
        child: Row(
          children: [
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  const SizedBox(height: 4),
                  // The only screen in the app with no ScreenHeader, so the
                  // shop's name is its h1 — without one a screen reader has no
                  // top of page to jump to. It sits outside the address
                  // control rather than inside it: a heading that is also a
                  // button is neither, and tapping the word "Unimiunte" opened
                  // the address picker, which it has nothing to do with.
                  Semantics(
                    headingLevel: 1,
                    container: true,
                    child: Text(
                      'Unimiunte',
                      style: TextStyle(
                        fontFamily: 'InterTight',
                        fontSize: 22,
                        height: 28 / 22,
                        fontWeight: FontWeight.w600,
                        letterSpacing: -.7,
                        color: ink,
                      ),
                    ),
                  ),
                  const SizedBox(height: 2),
                  ListenableBuilder(
                    listenable: AddressBook.instance,
                    builder: (context, _) {
                      final address = AddressBook.instance.selected;
                      return InkWell(
                        onTap: () => Navigator.push(
                          context,
                          MaterialPageRoute(
                            builder: (_) => const AddressesScreen(),
                          ),
                        ),
                        borderRadius: BorderRadius.circular(12),
                        child: Padding(
                          padding: const EdgeInsets.only(bottom: 4),
                          child: Row(
                            children: [
                              Icon(
                                LucideIcons.mapPin,
                                size: 15,
                                color: SeasonSkin.accent,
                              ),
                              const SizedBox(width: 5),
                              // Flexible, not Expanded: the chevron follows the
                              // text instead of being pushed to the far right
                              // edge. Pinned right it sat flush against the
                              // account avatar and read as that button's
                              // dropdown, when it has always belonged to the
                              // location it now sits beside.
                              Flexible(
                                child: Text(
                                  address == null
                                      ? 'Choose delivery location'
                                      : '${address.label.title} · ${address.line}',
                                  maxLines: 1,
                                  overflow: TextOverflow.ellipsis,
                                  style: TextStyle(
                                    fontFamily: 'InterTight',
                                    fontSize: 13.5,
                                    height: 18 / 13.5,
                                    letterSpacing: .2,
                                    // The ink, softened. A season supplies one
                                    // readable foreground and this is the
                                    // quieter half of it.
                                    color: ink.withValues(alpha: .82),
                                  ),
                                ),
                              ),
                              const SizedBox(width: 4),
                              Icon(
                                LucideIcons.chevronDown,
                                size: 16,
                                color: SeasonSkin.accent,
                              ),
                            ],
                          ),
                        ),
                      );
                    },
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _SearchLaunch extends StatefulWidget {
  final VoidCallback onTap;
  const _SearchLaunch({required this.onTap});

  @override
  State<_SearchLaunch> createState() => _SearchLaunchState();
}

class _SearchLaunchState extends State<_SearchLaunch> {
  int _hint = 0;
  Timer? _rotate;

  /// Slower than the campaign deck. This is a hint under a cursor, not a
  /// billboard, and text that changes while you are reading it is worse than
  /// text that does not change at all.
  static const _dwell = Duration(seconds: 4);

  List<String> get _hints => Seasons.instance.current?.hints ?? const [];

  @override
  void initState() {
    super.initState();
    if (_hints.length > 1) {
      _rotate = Timer.periodic(_dwell, (_) {
        if (mounted) setState(() => _hint = (_hint + 1) % _hints.length);
      });
    }
  }

  @override
  void dispose() {
    _rotate?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final disabled = MediaQuery.disableAnimationsOf(context);
    if (disabled && _rotate != null) {
      _rotate!.cancel();
      _rotate = null;
    }
    // During a festival the placeholder sells: Blinkit cycles "decorative
    // lights" and "ganesh idol" through this field, which costs nothing and
    // is the most-looked-at line on the screen. Outside a season it says what
    // the field does, which is what a search field should say.
    final hints = _hints;
    final label = hints.isEmpty
        ? 'Search products, shops and more'
        : 'Search "${hints[_hint % hints.length]}"';
    return ElevatedSurface(
      radius: LamazonTheme.featuredRadius,
      onTap: widget.onTap,
      semanticLabel: 'Search products and stores',
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 14),
      prominent: true,
      child: Row(
        children: [
          const Icon(LucideIcons.search, size: 21, color: LamazonTheme.strong),
          const SizedBox(width: 12),
          Expanded(
            // Cross-faded, and the label is excluded from semantics because
            // the surface above already names this control — a placeholder
            // that changes every four seconds should not re-announce itself
            // to a screen reader each time.
            child: ExcludeSemantics(
              child: AnimatedSwitcher(
                duration: Duration(milliseconds: disabled ? 0 : 260),
                child: Text(
                  label,
                  key: ValueKey(label),
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: const TextStyle(
                    fontFamily: 'InterTight',
                    fontSize: 15,
                    letterSpacing: .2,
                    color: LamazonTheme.muted,
                  ),
                ),
              ),
            ),
          ),
          const Icon(
            LucideIcons.arrowUpRight,
            size: 18,
            color: LamazonTheme.strong,
          ),
        ],
      ),
    );
  }
}

class _DepartmentStrip extends StatelessWidget {
  final int active;
  final ValueChanged<int> onSelect;
  const _DepartmentStrip({required this.active, required this.onSelect});

  @override
  // 104 rather than 92: the labels below the tiles get two lines now, and a
  // strip sized for one clipped the second.
  Widget build(BuildContext context) => SizedBox(
    height: 104,
    child: ListView.separated(
      scrollDirection: Axis.horizontal,
      clipBehavior: Clip.none,
      itemCount: departments.length,
      padding: const EdgeInsets.only(bottom: 8),
      separatorBuilder: (_, _) => const SizedBox(width: 11),
      itemBuilder: (context, index) {
        final department = departments[index];
        final selected = index == active;
        return Semantics(
          selected: selected,
          button: true,
          label: department.name,
          child: SizedBox(
            // 74, so "Stationery & Games" and "Household Essentials" fit on
            // two lines instead of ellipsing on both.
            width: 74,
            child: InkWell(
              onTap: () => onSelect(index),
              borderRadius: BorderRadius.circular(14),
              // The tap target is the whole column; the hover wash over that
              // much area reads as a grey slab, so only the press shows.
              hoverColor: Colors.transparent,
              // The contents are silenced, not the InkWell: otherwise the
              // tile announced itself three times over ("Electronics,
              // Electronics category, Electronics") — and, once the exclusion
              // moved up onto the whole widget, stopped being reachable by
              // keyboard at all.
              child: ExcludeSemantics(
                child: Column(
                  children: [
                    AnimatedContainer(
                      duration: Duration(
                        milliseconds: MediaQuery.disableAnimationsOf(context)
                            ? 0
                            : 180,
                      ),
                      width: 54,
                      height: 54,
                      decoration: BoxDecoration(
                        color: selected
                            ? SeasonSkin.accent
                            : LamazonTheme.surface,
                        borderRadius: BorderRadius.circular(16),
                        boxShadow: selected
                            ? LamazonTheme.tactileShadows
                            : LamazonTheme.surfaceShadows,
                      ),
                      clipBehavior: Clip.antiAlias,
                      child: index == 0
                          ? Icon(
                              department.icon,
                              color: selected
                                  ? SeasonSkin.onAccent
                                  : LamazonTheme.strong,
                            )
                          : CategoryVisual(
                              name: department.name,
                              imageUrl: department.imageUrl,
                            ),
                    ),
                    const SizedBox(height: 6),
                    // Two lines. At 66px and one line, four of the nine names
                    // truncated — "Household…", "Grocery & …", "Snacks & …",
                    // "Stationery …" — which is navigation you cannot read.
                    Text(
                      department.name,
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                      textAlign: TextAlign.center,
                      style: TextStyle(
                        fontFamily: 'InterTight',
                        fontSize: 11,
                        height: 1.15,
                        letterSpacing: .1,
                        color: selected
                            ? LamazonTheme.strong
                            : LamazonTheme.text,
                        fontWeight: selected
                            ? FontWeight.w600
                            : FontWeight.w400,
                      ),
                    ),
                  ],
                ),
              ),
            ),
          ),
        );
      },
    ),
  );
}

/// One department's shelves, as a titled grid.
///
/// The home screen stacks one of these per department, which is how a dense
/// marketplace lets somebody scan the whole shop without choosing a
/// department first: the heading says where you are, the tiles say what is
/// inside, and the arrow drops you into that department scoped. It is not the
/// strip repeated — the strip lists departments, these tiles are the shelves
/// under one, and they carry the shop's own artwork.
class _CategorySection extends StatelessWidget {
  final String title;
  final String? subtitle;
  final String department;
  final List<CategoryNode> categories;
  final VoidCallback? onSeeAll;
  final void Function(String category, String department) onOpenCategory;

  /// How many shelves to draw, or null for all of them.
  final int? limit;
  const _CategorySection({
    required this.title,
    required this.department,
    required this.categories,
    required this.onOpenCategory,
    this.subtitle,
    this.onSeeAll,
    this.limit,
  });

  @override
  Widget build(BuildContext context) {
    if (categories.isEmpty) return const SizedBox.shrink();
    // A department with thirty shelves would push everything under it off the
    // page, so a section on the home board shows two clean rows and the
    // heading carries the rest. Scoped to one department there is nothing
    // below to protect, so it shows the lot.
    final shown = limit == null ? categories : categories.take(limit!).toList();
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        SectionHeading(title: title, subtitle: subtitle, onAction: onSeeAll),
        const SizedBox(height: 14),
        LayoutBuilder(
          builder: (context, constraints) {
            final count = constraints.maxWidth >= 1060
                ? 6
                : constraints.maxWidth >= 690
                ? 5
                : 3;
            return GridView.builder(
              shrinkWrap: true,
              padding: EdgeInsets.zero,
              physics: const NeverScrollableScrollPhysics(),
              gridDelegate: SliverGridDelegateWithFixedCrossAxisCount(
                crossAxisCount: count,
                mainAxisSpacing: 14,
                crossAxisSpacing: 12,
                childAspectRatio: .73,
              ),
              itemCount: shown.length,
              itemBuilder: (context, index) => _CategoryTile(
                entry: _CategoryEntry(
                  shown[index].name,
                  department,
                  shown[index].imageUrl,
                ),
                onTap: () => onOpenCategory(shown[index].name, department),
              ),
            );
          },
        ),
      ],
    );
  }
}

class _CategoryEntry {
  final String name;
  final String department;
  final String imageUrl;
  const _CategoryEntry(this.name, this.department, this.imageUrl);
}

class _CategoryTile extends StatelessWidget {
  final _CategoryEntry entry;
  final VoidCallback onTap;
  const _CategoryTile({required this.entry, required this.onTap});
  @override
  Widget build(BuildContext context) => Semantics(
    button: true,
    label: 'Browse ${entry.name}',
    child: DecoratedBox(
      decoration: BoxDecoration(
        borderRadius: BorderRadius.circular(LamazonTheme.smallRadius),
        boxShadow: LamazonTheme.surfaceShadows,
      ),
      // Same as the product card: the rounded background is painted by
      // borderRadius, the artwork clips itself, and nothing else reaches the
      // edge — so the anti-aliased clip was a per-frame saveLayer on every one
      // of the forty-eight tiles for nothing.
      child: Material(
        color: LamazonTheme.surface,
        borderRadius: BorderRadius.circular(LamazonTheme.smallRadius),
        child: InkWell(
          onTap: onTap,
          borderRadius: BorderRadius.circular(LamazonTheme.smallRadius),
          // The artwork and the caption below it both name the category, so
          // the contents are silenced — but only the contents. Excluding the
          // InkWell too is what took every tile out of the tab order.
          child: ExcludeSemantics(
            child: Padding(
              padding: const EdgeInsets.all(7),
              child: Column(
                children: [
                  Expanded(
                    child: ClipRRect(
                      borderRadius: BorderRadius.circular(9),
                      child: CategoryVisual(
                        name: entry.name,
                        imageUrl: entry.imageUrl,
                      ),
                    ),
                  ),
                  const SizedBox(height: 7),
                  Text(
                    entry.name,
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis,
                    textAlign: TextAlign.center,
                    style: const TextStyle(
                      fontFamily: 'InterTight',
                      fontSize: 12,
                      height: 15 / 12,
                      fontWeight: FontWeight.w600,
                      letterSpacing: .1,
                    ),
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

class _StoreRail extends StatelessWidget {
  final List<Shop> shops;
  const _StoreRail({required this.shops});

  @override
  Widget build(BuildContext context) => SizedBox(
    height: 116,
    child: ListView.separated(
      scrollDirection: Axis.horizontal,
      clipBehavior: Clip.none,
      padding: const EdgeInsets.only(bottom: 10),
      itemCount: shops.length,
      separatorBuilder: (_, _) => const SizedBox(width: 12),
      itemBuilder: (context, index) => _StoreTile(shop: shops[index]),
    ),
  );
}

class _StoreTile extends StatelessWidget {
  final Shop shop;
  const _StoreTile({required this.shop});
  @override
  Widget build(BuildContext context) => SizedBox(
    width: 260,
    child: ElevatedSurface(
      radius: LamazonTheme.smallRadius,
      onTap: () => Navigator.push(
        context,
        MaterialPageRoute(builder: (_) => ShopScreen(shop: shop)),
      ),
      padding: const EdgeInsets.all(10),
      child: Row(
        children: [
          ClipRRect(
            borderRadius: BorderRadius.circular(9),
            child: SizedBox(
              width: 84,
              height: 84,
              child: NetImage(url: thumb(shop.imageUrl, 160), padTo: 16 / 9),
            ),
          ),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              mainAxisAlignment: MainAxisAlignment.center,
              children: [
                Text(
                  shop.name,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: const TextStyle(
                    fontFamily: 'InterTight',
                    fontSize: 15,
                    fontWeight: FontWeight.w600,
                    letterSpacing: -.15,
                  ),
                ),
                const SizedBox(height: 3),
                Text(
                  shop.tagline,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: const TextStyle(
                    fontFamily: 'InterTight',
                    fontSize: 12.5,
                    height: 15 / 12.5,
                    letterSpacing: .15,
                    color: LamazonTheme.muted,
                  ),
                ),
                const SizedBox(height: 6),
                const Row(
                  children: [
                    Icon(
                      LucideIcons.mapPin,
                      size: 13,
                      color: LamazonTheme.strong,
                    ),
                    SizedBox(width: 4),
                    Flexible(
                      child: Text(
                        'Local delivery',
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: TextStyle(
                          fontFamily: 'InterTight',
                          fontSize: 11.5,
                          fontWeight: FontWeight.w600,
                          color: LamazonTheme.strong,
                        ),
                      ),
                    ),
                  ],
                ),
              ],
            ),
          ),
        ],
      ),
    ),
  );
}

class _NothingHere extends StatelessWidget {
  final String tab;
  const _NothingHere({required this.tab});

  @override
  Widget build(BuildContext context) {
    final scoped = tab.isNotEmpty && tab != 'All';
    return ElevatedSurface(
      radius: LamazonTheme.featuredRadius,
      padding: const EdgeInsets.all(28),
      child: Column(
        children: [
          const Icon(LucideIcons.store, size: 38, color: LamazonTheme.strong),
          const SizedBox(height: 14),
          Text(
            scoped ? 'Nothing in $tab yet' : 'No shops open yet',
            textAlign: TextAlign.center,
            style: LamazonTheme.titleText,
          ),
          const SizedBox(height: 7),
          Text(
            scoped
                ? 'Try another department, or open a store here yourself.'
                : 'The first local store to open will appear here.',
            textAlign: TextAlign.center,
            style: LamazonTheme.mutedBodyText,
          ),
          const SizedBox(height: 20),
          ActionButton(
            label: 'Open your store',
            onPressed: () => Navigator.push(
              context,
              MaterialPageRoute(builder: (_) => const SellerDashboardScreen()),
            ),
          ),
        ],
      ),
    );
  }
}

class _HomeClose extends StatelessWidget {
  const _HomeClose();
  @override
  Widget build(BuildContext context) => DecoratedBox(
    decoration: BoxDecoration(
      color: LamazonTheme.forest,
      borderRadius: BorderRadius.circular(LamazonTheme.featuredRadius),
      boxShadow: LamazonTheme.raisedShadows,
    ),
    child: Padding(
      padding: const EdgeInsets.all(22),
      child: Row(
        children: [
          const Expanded(
            child: Text(
              'Local stores.\nGood things, close by.',
              style: TextStyle(
                fontFamily: 'InterTight',
                fontSize: 22,
                height: 28 / 22,
                fontWeight: FontWeight.w600,
                letterSpacing: -.7,
                color: Colors.white,
              ),
            ),
          ),
          TactileIconButton(
            icon: Icons.arrow_upward,
            label: 'Back to top',
            background: LamazonTheme.lime,
            foreground: LamazonTheme.strong,
            onPressed: () {
              final state = context.findAncestorStateOfType<_HomeScreenState>();
              if (state == null) return;
              if (MediaQuery.disableAnimationsOf(context)) {
                state._scroll.jumpTo(0);
              } else {
                state._scroll.animateTo(
                  0,
                  duration: const Duration(milliseconds: 260),
                  curve: Curves.easeOutCubic,
                );
              }
            },
          ),
        ],
      ),
    ),
  );
}
