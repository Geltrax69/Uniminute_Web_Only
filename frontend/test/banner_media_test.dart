import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:network_image_mock/network_image_mock.dart';
import 'package:lamazon/data/catalog.dart';
import 'package:lamazon/widgets/banner_media.dart';

/// A banner may be a photograph, a GIF or a short clip, and which one it is
/// is read off the URL rather than stored beside it.
///
/// The two things that have to hold: an animation must not be flattened on
/// the way out, and a clip must never be the reason the home screen is blank.

const _cloud = 'https://res.cloudinary.com/dq3da5bkb';
const _photo = '$_cloud/image/upload/v1/Unimiunte/Campaigns/diwali.jpg';
const _gif = '$_cloud/image/upload/v1/Unimiunte/Campaigns/diwali.gif';
const _clip = '$_cloud/video/upload/v1/Unimiunte/Campaigns/diwali.mp4';

String _urlOf(WidgetTester tester) =>
    (tester.widget<Image>(find.byType(Image)).image as NetworkImage).url;

Widget _banner(String url, {bool reduceMotion = false}) => MaterialApp(
  home: MediaQuery(
    data: MediaQueryData(disableAnimations: reduceMotion),
    child: Center(
      child: SizedBox(width: 600, height: 300, child: BannerMedia(url: url)),
    ),
  ),
);

void main() {
  test('the kind is read off the URL', () {
    expect(bannerKind(_photo), BannerKind.picture);
    expect(bannerKind(_gif), BannerKind.animation);
    expect(bannerKind(_clip), BannerKind.video);
    // Not everything is ours, and a bare extension is still an answer.
    expect(bannerKind('https://example.test/promo.webm'), BannerKind.video);
    expect(bannerKind('https://example.test/promo.png'), BannerKind.picture);
    expect(bannerKind(''), BannerKind.picture);
    // A query string is not part of the path and must not decide the kind.
    expect(
      bannerKind('https://example.test/p.png?from=x.mp4'),
      BannerKind.picture,
    );
  });

  test('a GIF is delivered as a clip, not as a GIF', () {
    // Measured on the same 140-frame animation: 6.2 MB as a GIF, 134 KB as
    // H.264 at the same width. That is the whole reason this path exists.
    final moving = bannerVideo(_gif);
    expect(moving, contains('f_mp4'));
    expect(moving, contains('c_limit,w_1280'));
    expect(moving, contains('ac_none'), reason: 'a shop must not make a noise');
    expect(
      moving,
      endsWith('/v1/Unimiunte/Campaigns/diwali.gif'),
      reason: 'the original is still what is addressed',
    );
  });

  test('anything with frames has a still, and the still is a JPEG', () {
    // Not f_auto: Cloudinary picks a format from the Accept header, and
    // Flutter fetches images over XHR, which sends */*. One 400px GIF came
    // back at 6.2 MB in this app for exactly that reason.
    final gif = bannerArtwork(_gif);
    expect(gif, contains('pg_1'), reason: 'one page of an animation');
    expect(gif, contains('f_jpg'));
    expect(gif, isNot(contains('f_auto')));
    expect(
      gif,
      isNot(contains('e_improve')),
      reason: 'a per-frame effect bills per frame, on a backdrop',
    );

    final clip = bannerArtwork(_clip);
    expect(clip, contains('/video/upload/so_0,'), reason: 'its first frame');
    expect(clip, contains('f_jpg'), reason: 'a .mp4 URL that is not a video');

    // A photograph has no frames to give up, so nothing changes.
    expect(bannerArtwork(_photo, still: true), bannerArtwork(_photo));
    expect(bannerArtwork(_photo), contains('c_limit,w_1600'));
  });

  test('a clip is capped and silent', () {
    final video = bannerVideo(_clip);
    expect(video, contains('ac_none'));
    expect(video, contains('c_limit,w_1280'));
    expect(video, endsWith('/v1/Unimiunte/Campaigns/diwali.mp4'));
  });

  test('what is not ours, and what is already done, is left alone', () {
    for (final url in ['', 'https://example.test/promo.gif']) {
      expect(bannerArtwork(url), url);
      expect(bannerVideo(url), url);
    }
    final once = bannerArtwork(_gif);
    expect(bannerArtwork(once), once, reason: 'a second cap would cap the cap');
    for (final clip in [bannerVideo(_clip), bannerVideo(_gif)]) {
      expect(bannerVideo(clip), clip);
    }
  });

  testWidgets('a GIF we host is played, a GIF we do not is drawn', (
    tester,
  ) async {
    await mockNetworkImagesFor(() async {
      // Ours: the player takes it, and the poster stands in until it starts.
      await tester.pumpWidget(_banner(_gif));
      await tester.pumpAndSettle();
      expect(_urlOf(tester), bannerArtwork(_gif));

      // Somebody else's: nothing can re-encode it, so it is drawn as the
      // animation it is and Image.network runs the frames.
      const foreign = 'https://example.test/promo.gif';
      await tester.pumpWidget(_banner(foreign));
      await tester.pumpAndSettle();
      expect(_urlOf(tester), foreign);
    });
  });

  testWidgets('reduced motion gets a clip as its first frame, not a player', (
    tester,
  ) async {
    await mockNetworkImagesFor(() async {
      await tester.pumpWidget(_banner(_clip, reduceMotion: true));
      await tester.pump();
      // No player is even constructed: WCAG 2.2.2 answered before it starts.
      expect(_urlOf(tester), bannerArtwork(_clip));
      expect(_urlOf(tester), contains('so_0'));
    });
  });

  testWidgets('a clip that will not play leaves the poster up', (tester) async {
    await mockNetworkImagesFor(() async {
      // There is no video platform in a widget test, so initialize() throws —
      // which is the same shape as a dead URL or a codec nobody has.
      await tester.pumpWidget(_banner(_clip));
      await tester.pumpAndSettle();
      expect(
        tester.takeException(),
        isNull,
        reason: 'a banner is not worth a broken home screen',
      );
      expect(find.byType(Image), findsOneWidget);
      expect(_urlOf(tester), contains('so_0'));
    });
  });
}
