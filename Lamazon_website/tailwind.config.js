/** @type {import('tailwindcss').Config} */
//
// Every value below is lifted verbatim from the Flutter app:
//   colours, radii, shadows  -> frontend/lib/widgets/design_system.dart  (LamazonTheme)
//   type scale               -> design_system.dart textTheme + ActionButton/ProductCard
//   breakpoints              -> frontend/lib/widgets/app_shell.dart (_tablet = 700)
//   gutter scale             -> design_system.dart LamazonTheme.gutter (16 / 24 / 32)
// Nothing is approximated; where Tailwind's default scale disagreed with the
// app (md 768 vs the app's 700 tablet line) the app's number wins.
module.exports = {
  content: [
    "./templates/**/*.templ",
    "./templates/**/*.go",
    "./*.go",
    "./static/js/**/*.js",
  ],
  theme: {
    screens: {
      // Phone-first, exactly the app's own lines:
      //  480  large phones get the wider two-column arrangement
      //  700  app_shell._tablet — where Flutter flips to isWide()
      // 1200  LamazonTheme.gutter's last step, and where the header gains its desktop nav
      sm: "480px",
      md: "700px",
      lg: "1024px",
      xl: "1200px",
    },
    extend: {
      colors: {
        // LamazonTheme.* — semantic names kept 1:1 so a template line can be
        // diffed against a Flutter line.
        text: "#17221D",
        muted: "#66716A",
        strong: "#1D4939",
        track: "#E1E5DD",
        canvas: "#F7F6F0",
        surface: "#FFFDF8",
        forest: "#143E32",
        lime: "#C6EE63",
        peach: "#F58268",
        danger: "#B93643",
        warning: "#9A5B12",
        // Companion constants that live next to them in the Flutter code.
        "price-old": "#8A8A8A", // PriceLine struck-through MRP
        "img-ground": "#F0F1EB", // NetImage placeholder ground
        "skeleton-a": "#F0F0E9", // Skeleton gradient stops
        "skeleton-b": "#E5E8DF",
        "wash-strong": "#0D2119", // every shadow's ink, at the listed alphas
      },
      fontFamily: {
        // assets/fonts/InterTight-Variable.ttf, the app's only font family.
        sans: ["InterTight", "Inter Tight", "system-ui", "sans-serif"],
      },
      fontSize: {
        // The app's text styles, name-for-name from design_system.dart.
        // [size, {line-height, letter-spacing, weight}]
        "title": ["22px", { lineHeight: "28px", letterSpacing: "-0.7px", fontWeight: "600" }],
        "section": ["19px", { lineHeight: "24px", letterSpacing: "-0.6px", fontWeight: "600" }],
        "title-small": ["16px", { lineHeight: "20px", letterSpacing: "-0.2px", fontWeight: "600" }],
        "body": ["14.5px", { lineHeight: "18px", letterSpacing: "0.3px" }],
        "body-muted": ["14.5px", { lineHeight: "18px", letterSpacing: "0.3px", color: "#66716A" }],
        "label": ["14px", { lineHeight: "18px", letterSpacing: "0.15px", fontWeight: "600" }],
        "small": ["12.5px", { lineHeight: "16px", letterSpacing: "0.2px" }],
        // ProductCard / PriceLine one-offs.
        "price": ["17px", { lineHeight: "21px", letterSpacing: "-0.2px", fontWeight: "700" }],
        "price-mrp": ["12.5px", { lineHeight: "16px" }],
        "card-name": ["13px", { lineHeight: "16.5px", letterSpacing: "0.05px", fontWeight: "500" }],
        "card-store": ["11px", { lineHeight: "14px", letterSpacing: "0.2px" }],
        "card-note": ["11.5px", { lineHeight: "15px", fontWeight: "700" }],
        "strip-label": ["11px", { lineHeight: "1.15", letterSpacing: "0.1px" }],
        "header-addr": ["13.5px", { lineHeight: "18px", letterSpacing: "0.2px" }],
        "search-launch": ["15px", { letterSpacing: "0.2px" }],
      },
      borderRadius: {
        // LamazonTheme.radius / smallRadius / featuredRadius, plus the
        // DiscountBadge's 9 and the department tile's 14.
        DEFAULT: "16px",
        small: "12px",
        featured: "18px",
        badge: "9px",
        tile: "14px",
      },
      boxShadow: {
        // LamazonTheme.surfaceShadows / raisedShadows / tactileShadows,
        // token for token (ink 0x0D2119 at each listed alpha). Blur is Flutter's
        // blurRadius as CSS: 2 × (blurRadius × 0.57735 + 0.5).
        surface:
          "0 2px 6.8px -1px rgba(13,33,25,0.07), 0 13px 33.3px -10px rgba(13,33,25,0.075)",
        raised:
          "0 3px 7.9px -2px rgba(13,33,25,0.10), 0 10px 26.4px -10px rgba(13,33,25,0.09)",
        tactile:
          "0 4px 10.2px -2px rgba(13,33,25,0.12), 0 10px 21.8px -9px rgba(13,33,25,0.063)",
        badge: "0 2px 5px rgba(13,33,25,0.16)", // DiscountBadge 0x290D2119
      },
      maxWidth: {
        // AppShell caps the shop at 1400; the floating nav bar at 960;
        // ReadableBody keeps reading surfaces at 620.
        shell: "1400px",
        navbar: "960px",
        readable: "620px",
      },
      minHeight: {
        // FilledButtonTheme / IconButtonThemeData minimum sizes.
        button: "46px",
        touch: "48px",
      },
    },
  },
  plugins: [],
};
