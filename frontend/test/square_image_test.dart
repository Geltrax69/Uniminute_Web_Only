import 'package:flutter_test/flutter_test.dart';
import 'package:lamazon/data/catalog.dart';

void main() {
  const cloud =
      'https://res.cloudinary.com/dq3da5bkb/image/upload/v1786549855/Unimiunte/PURE_BITES/burger.png';

  test('an upload is padded to a square for a tile', () {
    expect(
      padded(cloud),
      'https://res.cloudinary.com/dq3da5bkb/image/upload/'
      'c_pad,w_512,h_512,b_rgb:$padFill,f_auto,q_auto/'
      'v1786549855/Unimiunte/PURE_BITES/burger.png',
    );
  });

  test('a banner is padded wide, and wider means a wider source', () {
    // 16:9 at the banner width. A tile's 512 would be soft full-bleed.
    expect(padded(cloud, 16 / 9), contains('c_pad,w_1024,h_576'));
  });

  test('a portrait shape is allowed too', () {
    expect(padded(cloud, 3 / 4), contains('c_pad,w_512,h_683'));
  });

  test('transforming twice would scale the padding, so it is a no-op', () {
    expect(padded(padded(cloud)), padded(cloud));
    // Including across shapes: the first one wins rather than compounding.
    expect(padded(padded(cloud), 16 / 9), padded(cloud));
  });

  test('a non-Cloudinary url is left alone', () {
    // The sample catalogue is Unsplash, which has no /image/upload/ to splice.
    const unsplash = 'https://images.unsplash.com/photo-1441986300917?w=400';
    expect(padded(unsplash), unsplash);
  });

  test('an empty url stays empty rather than becoming a request', () {
    expect(padded(''), '');
  });
}
