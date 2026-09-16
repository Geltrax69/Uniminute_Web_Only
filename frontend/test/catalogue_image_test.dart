import 'dart:io';
import 'package:flutter_test/flutter_test.dart';
import 'package:lamazon/data/catalog.dart';
import 'package:lamazon/widgets/design_system.dart';

/// The catalogue transform, and the two properties that make it safe:
/// the seller's original is never rewritten, and the CDN is asked for a small
/// fixed set of sizes rather than one per device width.

const _real =
    'https://res.cloudinary.com/dq3da5bkb/image/upload/'
    'v1786549855/Unimiunte/PURE_BITES/PURE_BITES_Aloo_TIkki_Burger_1.png';

void main() {
  test('every tile is asked for as a centred, filled square', () {
    final url = catalogueImage(_real);
    expect(url, contains('c_fill'), reason: 'filled, so there are no bars');
    expect(url, contains('ar_1:1'), reason: 'square, so the grid agrees');
    expect(url, contains('g_auto'), reason: 'the subject, not the tray corner');
    expect(url, contains('f_auto'));
    expect(url, contains('q_auto'));
  });

  test('the original is left in the URL, never rewritten', () {
    // Cloudinary transforms are a prefix on the path. What the seller
    // uploaded is still addressable, which is what the details gallery uses.
    expect(
      catalogueImage(_real),
      contains(
        'v1786549855/Unimiunte/PURE_BITES/PURE_BITES_Aloo_TIkki_Burger_1.png',
      ),
    );
    expect(
      catalogueImage(_real).startsWith('https://res.cloudinary.com/'),
      isTrue,
    );
  });

  test('an already-transformed URL is left alone', () {
    // Otherwise a second pass crops the crop, and the third crops that.
    for (final spec in ['c_fill,ar_1:1', 'c_pad,w_400', 'c_limit,w_800']) {
      final once = _real.replaceFirst('/image/upload/', '/image/upload/$spec/');
      expect(catalogueImage(once), once);
    }
  });

  test('anything that is not a Cloudinary upload passes straight through', () {
    for (final url in [
      '',
      'https://example.test/photo.jpg',
      'assets/categories/campaign-forest.webp',
    ]) {
      expect(catalogueImage(url), url);
    }
  });

  test('widths land in a small fixed set, so the CDN can cache them', () {
    // Cloudinary bills and caches per derived image, keyed on the exact URL.
    // w_161 on one phone and w_163 on the next is two transformations, two
    // cache entries and two charges for a picture nobody can tell apart.
    final widths = <String>{};
    for (var w = 1; w <= 1200; w++) {
      final url = catalogueImage(_real, w);
      widths.add(RegExp(r'w_(\d+)').firstMatch(url)!.group(1)!);
    }
    expect(
      widths,
      {'160', '300', '400', '800', '1200'},
      reason: 'the whole catalogue settles into five derivatives per photo',
    );
  });

  test('a bucket is never smaller than what was asked for', () {
    // Rounding down would be visibly soft, which is the one thing this must
    // not trade away for a cache hit.
    for (final asked in [1, 159, 160, 161, 299, 300, 401, 799, 801]) {
      final got = int.parse(
        RegExp(r'w_(\d+)').firstMatch(catalogueImage(_real, asked))!.group(1)!,
      );
      expect(got, greaterThanOrEqualTo(asked), reason: 'asked for $asked');
    }
  });

  /// A payload ratchet, in the spirit of palette_ratchet_test: a number that
  /// may fall and may not rise.
  ///
  /// The cold first load was 8.64 MB, and 4 MB of it was two decorative
  /// photographs shipped as PNG. Nothing in a pull request shows an asset
  /// getting heavier, so this does. Lower the budget as assets shrink; never
  /// raise it to make a build pass.
  test('the bundled artwork stays inside its weight budget', () {
    const budgetBytes = 400 * 1024;
    final assets = Directory('assets')
        .listSync(recursive: true)
        .whereType<File>()
        .where((f) => !f.path.endsWith('.ttf'))
        .toList();
    final total = assets.fold<int>(0, (sum, f) => sum + f.lengthSync());
    expect(
      total,
      lessThanOrEqualTo(budgetBytes),
      reason:
          'bundled images total ${(total / 1024).round()} KB:\n'
          '${assets.map((f) => '  ${f.path} '
              '${(f.lengthSync() / 1024).round()} KB').join('\n')}',
    );
    for (final file in assets) {
      expect(
        file.path.endsWith('.png') && file.lengthSync() > 200 * 1024,
        isFalse,
        reason: '${file.path} is a large PNG; a photograph belongs in WebP',
      );
    }
  });

  test('the letterbox is the app\'s own surface, not the photograph\'s edge', () {
    // b_auto sampled the picture: a portrait shot on a wooden table gave the
    // product page muddy olive-brown bars that are in no palette we own.
    expect(
      '#$padFill'.toUpperCase(),
      '#${(LamazonTheme.surface.toARGB32() & 0xFFFFFF).toRadixString(16).padLeft(6, '0').toUpperCase()}',
      reason: 'data/ cannot import the theme, so the two are checked here',
    );
    expect(padded(_real, 1, 1024), contains('b_rgb:$padFill'));
    expect(padded(_real, 1, 1024), isNot(contains('b_auto')));
  });
}
