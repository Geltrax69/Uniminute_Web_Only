class Product {
  final String id;
  final String name;
  final String category;
  final String tab; // which top tab this belongs to; 'All' tab shows everything
  final double price;
  final int? availableStock;

  /// Price before the discount. Zero means the seller is not running one.
  final double mrp;
  final String imageUrl; // any web image link works here
  final String store;
  final String description;
  final List<String> sizes;
  final List<String> extraImages;
  final List<ShopOffer> offers; // same product priced at other shops
  /// What the shop asks the buyer to choose. Empty for most things.
  final List<ItemOption> options;

  /// Which products this one is comparable to. Categories say where to browse;
  /// this says what can be lined up side by side, which is a different
  /// question — two shops may shelve the same charger differently.
  final String compareGroup;
  final Map<String, String> attributes;

  const Product({
    required this.id,
    required this.name,
    required this.category,
    this.tab = 'All',
    required this.price,
    this.availableStock,
    this.mrp = 0,
    required this.imageUrl,
    this.store = 'Unimiunte Store',
    required this.description,
    this.sizes = const [],
    this.extraImages = const [],
    this.offers = const [],
    this.options = const [],
    this.compareGroup = '',
    this.attributes = const {},
  });

  /// Local basket snapshot. Checkout still uses authoritative server prices.
  Map<String, dynamic> toJson() => {
    'id': id,
    'name': name,
    'category': category,
    'tab': tab,
    'price': price,
    'availableStock': availableStock,
    'mrp': mrp,
    'imageUrl': imageUrl,
    'store': store,
    'description': description,
    'sizes': sizes,
    'extraImages': extraImages,
    'offers': [
      for (final o in offers) {'store': o.store, 'price': o.price},
    ],
    'options': [for (final o in options) o.toJson()],
    'compareGroup': compareGroup,
    'attributes': attributes,
  };

  factory Product.fromJson(Map<String, dynamic> r) => Product(
    id: r['id'] as String,
    name: r['name'] as String,
    category: r['category'] as String,
    tab: r['tab'] as String,
    price: (r['price'] as num).toDouble(),
    availableStock: (r['availableStock'] as num?)?.toInt(),
    mrp: (r['mrp'] as num).toDouble(),
    imageUrl: r['imageUrl'] as String,
    store: r['store'] as String,
    description: r['description'] as String,
    sizes: (r['sizes'] as List).cast<String>(),
    extraImages: (r['extraImages'] as List).cast<String>(),
    offers: [
      for (final o in r['offers'] as List)
        ShopOffer(o['store'] as String, (o['price'] as num).toDouble()),
    ],
    options: [
      for (final o in r['options'] as List)
        ItemOption.fromJson(Map<String, dynamic>.from(o as Map)),
    ],
    compareGroup: r['compareGroup'] as String,
    attributes: Map<String, String>.from(r['attributes'] as Map),
  );

  /// True only when there is a real saving to show. An MRP equal to the price
  /// is a sale the seller has ended, not a 0% one worth a badge.
  bool get discounted => mrp > price;

  /// Whole percent off, rounded the way a shopper reads it. 199 from 249 is
  /// "20% off", not "20.08%".
  int get discountPercent =>
      discounted ? (((mrp - price) / mrp) * 100).round() : 0;

  /// What the shopper picks. The bundled catalogue predates option groups and
  /// carries a plain [sizes] list; rather than leave that data stranded, it
  /// becomes a Size group so both kinds of product render through one path.
  List<ItemOption> get choices => options.isNotEmpty
      ? options
      : sizes.isEmpty
      ? const []
      : [ItemOption(name: 'Size', values: sizes)];
}

/// One thing a buyer picks before ordering, and the choices the shop offers.
/// [kind] is 'colour' when the values are hex and should draw as swatches.
class ItemOption {
  final String name;
  final String kind;
  final List<String> values;
  const ItemOption({
    required this.name,
    this.kind = 'text',
    this.values = const [],
  });

  bool get isColour => kind == 'colour';

  factory ItemOption.fromJson(Map<String, dynamic> r) => ItemOption(
    name: r['name'] as String? ?? '',
    kind: r['kind'] as String? ?? 'text',
    values: (r['values'] as List<dynamic>? ?? const []).cast<String>(),
  );

  Map<String, dynamic> toJson() => {
    'name': name,
    'kind': kind,
    'values': values,
  };
}

/// A set of comparable products and the fields to compare them on.
class CompareGroup {
  final String name;
  final List<GroupAttribute> attributes;
  final int items;
  const CompareGroup(this.name, this.attributes, [this.items = 0]);

  factory CompareGroup.fromJson(Map<String, dynamic> r) =>
      CompareGroup(r['name'] as String? ?? '', [
        for (final a in (r['attributes'] as List<dynamic>? ?? const []))
          GroupAttribute.fromJson(a as Map<String, dynamic>),
      ], (r['items'] as num?)?.toInt() ?? 0);
}

/// What "better" means for one compared field. Empty is deliberate and means
/// "show it, do not rank it" — a template written before modes existed keeps
/// working, and nothing invents a winner for brand or flavour.
class CompareMode {
  static const none = '';
  static const higher = 'higher_better';
  static const lower = 'lower_better';
  static const feature = 'feature';
  static const info = 'info';

  /// What the admin picks from, and what each one reads as in the editor.
  static const labels = {
    none: 'Not ranked',
    higher: 'Higher is better',
    lower: 'Lower is better',
    feature: 'Has it or not',
    info: 'Just show it',
  };
}

class GroupAttribute {
  final String name;
  final String unit;
  final String mode;

  /// The field the price is divided by for the ₹/100 g row. One per group.
  final bool perUnit;

  /// Product id that won this field on the compare screen, filled in by the
  /// API. Empty is the common answer: everyone tied, only one product
  /// answered, nobody did, or the field is not the ranking kind.
  final String winner;

  const GroupAttribute(
    this.name, [
    this.unit = '',
    this.mode = CompareMode.none,
    this.perUnit = false,
    this.winner = '',
  ]);

  factory GroupAttribute.fromJson(Map<String, dynamic> r) => GroupAttribute(
    r['name'] as String? ?? '',
    r['unit'] as String? ?? '',
    r['mode'] as String? ?? '',
    r['perUnit'] as bool? ?? false,
    r['winner'] as String? ?? '',
  );

  /// The winner is decided per request and never sent back, so it stays out.
  Map<String, dynamic> toJson() => {
    'name': name,
    'unit': unit,
    if (mode.isNotEmpty) 'mode': mode,
    if (perUnit) 'perUnit': true,
  };

  GroupAttribute copyWith({
    String? name,
    String? unit,
    String? mode,
    bool? perUnit,
  }) => GroupAttribute(
    name ?? this.name,
    unit ?? this.unit,
    mode ?? this.mode,
    perUnit ?? this.perUnit,
  );

  /// Whether the compare screen picks a winner for this field. Only these two
  /// modes rank; feature, info and unset are shown and left alone.
  bool get ranked => mode == CompareMode.higher || mode == CompareMode.lower;

  /// "20" plus "W" reads as 20W; a field with no unit is left alone.
  String show(String value) =>
      value.isEmpty ? '—' : (unit.isEmpty ? value : '$value$unit');
}

/// A row nobody typed: price per unit, computed per request from the price and
/// the quantity field, so it moves the moment a seller changes a price.
class DerivedRow {
  final String name;
  final Map<String, double> values;
  final String winner;
  const DerivedRow(this.name, this.values, this.winner);

  factory DerivedRow.fromJson(Map<String, dynamic> r) =>
      DerivedRow(r['name'] as String? ?? '', {
        for (final e in (r['values'] as Map? ?? {}).entries)
          '${e.key}': (e.value as num).toDouble(),
      }, r['winner'] as String? ?? '');
}

class ShopOffer {
  final String store;
  final double price;
  const ShopOffer(this.store, this.price);
}

class Shop {
  final String name;
  final String tagline;
  final String imageUrl;
  final String tab;

  const Shop({
    required this.name,
    required this.tagline,
    required this.imageUrl,
    this.tab = 'All',
  });
}
