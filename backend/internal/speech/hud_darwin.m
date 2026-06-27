// +build darwin

#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>
#import <QuartzCore/QuartzCore.h>

// ═══════════════════════════════════════════════════════════════════════
// Quick Agent HUD — a floating, frameless panel hosting a WKWebView that
// renders the Svelte "#hud" route. The web content is served by Flow's
// internal localhost HUD server; this native layer only owns the window.
//
// Communication is handled entirely inside the web page:
//   • Go → JS : Server-Sent Events  (GET  /api/hud/events)
//   • JS → Go : fetch POST          (POST /api/hud/ask, /approve, /open …)
//
// so this file deliberately contains no JS↔native bridge — just window
// lifecycle, positioning and show/hide animation.
// ═══════════════════════════════════════════════════════════════════════

// A borderless panel must opt in to becoming key so the HUD text field can
// accept keyboard input.
@interface FlowHUDPanel : NSPanel
@end
@implementation FlowHUDPanel
- (BOOL)canBecomeKeyWindow { return YES; }
- (BOOL)canBecomeMainWindow { return NO; }
@end

static FlowHUDPanel *hudPanel   = nil;
static WKWebView    *hudWebView = nil;
static NSString     *hudLoadedURL = nil;

static const CGFloat HUD_W       = 540;
static const CGFloat HUD_H       = 120;   // initial/compact height; JS drives the rest
static const CGFloat HUD_TOP_GAP = 12;    // distance below the top of the screen

// Position the panel horizontally centered, anchored near the top of the
// screen that currently holds the mouse (mirrors the mockups' notch area).
static NSRect hudFrameForSize(CGFloat w, CGFloat h) {
    NSScreen *scr = nil;
    NSPoint mouse = [NSEvent mouseLocation];
    for (NSScreen *s in [NSScreen screens]) {
        if (NSPointInRect(mouse, s.frame)) { scr = s; break; }
    }
    if (!scr) scr = [NSScreen mainScreen];
    if (!scr) scr = [[NSScreen screens] firstObject];

    NSRect vf = scr ? scr.visibleFrame : NSMakeRect(0, 0, 1440, 900);
    CGFloat x = vf.origin.x + (vf.size.width - w) / 2.0;
    CGFloat y = NSMaxY(vf) - h - HUD_TOP_GAP;
    return NSMakeRect(x, y, w, h);
}

static void ensureHUD(void) {
    if (hudPanel) return;

    NSRect frame = hudFrameForSize(HUD_W, HUD_H);

    hudPanel = [[FlowHUDPanel alloc]
        initWithContentRect:frame
                  styleMask:NSWindowStyleMaskBorderless | NSWindowStyleMaskNonactivatingPanel
                    backing:NSBackingStoreBuffered
                      defer:YES];
    hudPanel.level                    = NSFloatingWindowLevel;
    hudPanel.opaque                   = NO;
    hudPanel.backgroundColor          = [NSColor clearColor];
    hudPanel.hasShadow                = YES;
    hudPanel.movableByWindowBackground = YES;
    hudPanel.hidesOnDeactivate        = NO;
    hudPanel.floatingPanel            = YES;
    // Only take key focus when a control that needs the keyboard (the text
    // field) is actually clicked — so showing the HUD and clicking its buttons
    // never activates Flow or pulls its main window to the front.
    hudPanel.becomesKeyOnlyIfNeeded   = YES;
    hudPanel.collectionBehavior       = NSWindowCollectionBehaviorCanJoinAllSpaces
                                      | NSWindowCollectionBehaviorFullScreenAuxiliary;

    // Rounded, clipped container so the web content gets the HUD's corners.
    // A dark fill ensures that any area not yet covered by web content (e.g.
    // during a resize) reads as HUD background rather than a transparent gap.
    NSView *root = [[NSView alloc] initWithFrame:hudPanel.contentView.bounds];
    root.wantsLayer          = YES;
    root.layer.cornerRadius  = 18;
    root.layer.masksToBounds = YES;
    root.layer.backgroundColor = [NSColor colorWithCalibratedWhite:0.07 alpha:0.97].CGColor;
    root.autoresizingMask    = NSViewWidthSizable | NSViewHeightSizable;

    WKWebViewConfiguration *cfg = [[WKWebViewConfiguration alloc] init];
    cfg.suppressesIncrementalRendering = NO;

    hudWebView = [[WKWebView alloc] initWithFrame:root.bounds configuration:cfg];
    hudWebView.autoresizingMask = NSViewWidthSizable | NSViewHeightSizable;
    // Transparent so the page's own background (and the rounded corners) show.
    @try { [hudWebView setValue:@NO forKey:@"drawsBackground"]; } @catch (__unused NSException *e) {}
    if (@available(macOS 12.0, *)) {
        hudWebView.underPageBackgroundColor = [NSColor clearColor];
    }

    [root addSubview:hudWebView];
    hudPanel.contentView = root;
}

// ═══════════════════════════════════════════════════════════════════════
// Public C API (called from hud_darwin.go via CGO)
// ═══════════════════════════════════════════════════════════════════════

// Show the HUD, loading `url` if it isn't already loaded.
void FlowShowHUD(const char *curl) {
    NSString *url = curl ? [NSString stringWithUTF8String:curl] : @"";
    dispatch_async(dispatch_get_main_queue(), ^{
        ensureHUD();

        if (url.length > 0 && ![url isEqualToString:hudLoadedURL]) {
            NSURL *u = [NSURL URLWithString:url];
            if (u) {
                [hudWebView loadRequest:[NSURLRequest requestWithURL:u]];
                hudLoadedURL = [url copy];
            }
        }

        if (!hudPanel.isVisible || hudPanel.alphaValue < 0.1) {
            // Fresh show: re-anchor to a compact size near the top of the
            // active screen. JS resizes it to fit content shortly after.
            [hudPanel setFrame:hudFrameForSize(HUD_W, HUD_H) display:YES];
            hudPanel.alphaValue = 0.0;
            // orderFrontRegardless shows the HUD on top WITHOUT activating Flow
            // (the main window / dock never jumps forward). The panel only
            // becomes key when its text field is clicked (becomesKeyOnlyIfNeeded).
            [hudPanel orderFrontRegardless];
            [NSAnimationContext runAnimationGroup:^(NSAnimationContext *ctx) {
                ctx.duration = 0.15;
                hudPanel.animator.alphaValue = 1.0;
            } completionHandler:nil];
        } else {
            [hudPanel orderFrontRegardless];
        }
    });
}

// Resize the HUD to fit its content height, keeping the top edge anchored so it
// grows/shrinks downward. Driven by the web content as it changes.
void FlowResizeHUD(int height) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (!hudPanel) return;
        CGFloat h = height;
        if (h < 56)  h = 56;
        if (h > 560) h = 560;

        NSRect f = hudPanel.frame;
        CGFloat top = NSMaxY(f);              // keep the top edge fixed
        NSRect nf = NSMakeRect(f.origin.x, top - h, NSWidth(f), h);

        // Keep the panel on-screen if it would grow past the bottom.
        NSScreen *scr = hudPanel.screen ?: [NSScreen mainScreen];
        if (scr) {
            CGFloat minY = NSMinY(scr.visibleFrame) + 8;
            if (nf.origin.y < minY) nf.origin.y = minY;
        }

        // Skip tiny deltas to avoid jitter during streaming.
        if (fabs(NSHeight(f) - h) < 2.0) return;

        // Animate the height change so listening → thinking → answer grows/
        // shrinks smoothly as one panel.
        [NSAnimationContext runAnimationGroup:^(NSAnimationContext *ctx) {
            ctx.duration = 0.17;
            ctx.timingFunction = [CAMediaTimingFunction functionWithName:kCAMediaTimingFunctionEaseInEaseOut];
            [[hudPanel animator] setFrame:nf display:YES];
        } completionHandler:nil];
    });
}

// Pre-create the panel and load the web content while leaving it hidden, so the
// first real show (the "Listening" state) appears instantly.
void FlowPreloadHUD(const char *curl) {
    NSString *url = curl ? [NSString stringWithUTF8String:curl] : @"";
    dispatch_async(dispatch_get_main_queue(), ^{
        ensureHUD();
        if (url.length > 0 && ![url isEqualToString:hudLoadedURL]) {
            NSURL *u = [NSURL URLWithString:url];
            if (u) {
                [hudWebView loadRequest:[NSURLRequest requestWithURL:u]];
                hudLoadedURL = [url copy];
            }
        }
    });
}

void FlowHideHUD(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (!hudPanel || !hudPanel.isVisible) return;
        [NSAnimationContext runAnimationGroup:^(NSAnimationContext *ctx) {
            ctx.duration = 0.12;
            hudPanel.animator.alphaValue = 0.0;
        } completionHandler:^{
            [hudPanel orderOut:nil];
        }];
    });
}
