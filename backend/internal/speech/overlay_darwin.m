// +build darwin

#import <Cocoa/Cocoa.h>
#import <ApplicationServices/ApplicationServices.h>
#import <QuartzCore/QuartzCore.h>

// ═══════════════════════════════════════════════════════════════════════
// Dictation Overlay — a small floating pill that appears above the text
// cursor to give visual feedback while recording / transcribing.
//
//   state 1  →  ┌──────┐   waveform bars (recording)
//               └──────┘
//
//   state 2  →  ┌───────────────┐  ● Thinking…  (API call in progress)
//               └───────────────┘
// ═══════════════════════════════════════════════════════════════════════

// ── Waveform View (5 animated bars) ──────────────────────────────────

@interface DictationWaveformView : NSView {
    CGFloat _bars[5];
}
@property (nonatomic, strong) NSTimer *animTimer;
@end

@implementation DictationWaveformView

- (instancetype)initWithFrame:(NSRect)frame {
    self = [super initWithFrame:frame];
    if (self) {
        CGFloat seed[] = {0.35, 0.55, 0.75, 0.55, 0.35};
        for (int i = 0; i < 5; i++) _bars[i] = seed[i];
    }
    return self;
}

- (void)startAnimating {
    if (self.animTimer) return;
    __weak DictationWaveformView *weakSelf = self;
    self.animTimer = [NSTimer scheduledTimerWithTimeInterval:0.09
                                                    repeats:YES
                                                      block:^(NSTimer * _Nonnull t) {
        DictationWaveformView *ss = weakSelf;
        if (!ss) { [t invalidate]; return; }
        // Bias: middle bars tend to be taller.
        CGFloat bias[] = {0.30, 0.50, 0.70, 0.50, 0.30};
        for (int i = 0; i < 5; i++) {
            CGFloat target = bias[i] + (arc4random_uniform(60)) / 100.0;
            if (target > 1.0) target = 1.0;
            ss->_bars[i] += (target - ss->_bars[i]) * 0.55;
        }
        [ss setNeedsDisplay:YES];
    }];
}

- (void)stopAnimating {
    [self.animTimer invalidate];
    self.animTimer = nil;
}

- (void)drawRect:(NSRect)dirtyRect {
    CGFloat barW = 3.0, gap = 2.5;
    int n = 5;
    CGFloat totalW = n * barW + (n - 1) * gap;
    CGFloat startX = (NSWidth(self.bounds) - totalW) / 2.0;
    CGFloat maxH   = NSHeight(self.bounds) * 0.7;

    [[NSColor whiteColor] setFill];
    for (int i = 0; i < n; i++) {
        CGFloat h = MAX(3.0, maxH * _bars[i]);
        CGFloat x = startX + i * (barW + gap);
        CGFloat y = (NSHeight(self.bounds) - h) / 2.0;
        NSRect r = NSMakeRect(x, y, barW, h);
        [[NSBezierPath bezierPathWithRoundedRect:r xRadius:1.5 yRadius:1.5] fill];
    }
}

@end

// ── Overlay Panel (singleton) ────────────────────────────────────────

static NSPanel              *ovPanel       = nil;
static DictationWaveformView *waveView     = nil;
static NSProgressIndicator  *thinkSpinner  = nil;
static NSTextField          *thinkLabel    = nil;
static NSView               *recContainer  = nil;
static NSView               *thinkContainer = nil;
static NSTimer              *dotsTimer     = nil;
static int                   dotsCount     = 0;

static const CGFloat OV_REC_W   = 52,  OV_REC_H   = 34;
static const CGFloat OV_THINK_W = 138, OV_THINK_H = 36;
static const CGFloat OV_RADIUS  = 16;
static const CGFloat OV_GAP_Y   = 8;   // pixels above the caret

// ── Caret Position via Accessibility API ─────────────────────────────

static NSPoint getCaretPosition(void) {
    NSPoint fallback = [NSEvent mouseLocation];
    // Nudge fallback upward so the overlay doesn't sit right on the cursor arrow.
    fallback.y += 24;

    // AX APIs are slow (1-2s per call) without Accessibility permission,
    // and even with permission they add noticeable latency.  Skip entirely
    // when the app isn't trusted — the mouse position is a good fallback.
    if (!AXIsProcessTrusted()) return fallback;

    // AX APIs can fail or misbehave without Accessibility permission.
    // Guard every call and bail to the mouse-position fallback on any issue.
    AXUIElementRef sysWide = AXUIElementCreateSystemWide();
    if (!sysWide) return fallback;

    AXUIElementRef focusedApp = NULL;
    AXError err = AXUIElementCopyAttributeValue(sysWide, kAXFocusedApplicationAttribute,
                                                (CFTypeRef *)&focusedApp);
    if (err != kAXErrorSuccess || !focusedApp) {
        CFRelease(sysWide);
        return fallback;
    }

    AXUIElementRef focusedEl = NULL;
    err = AXUIElementCopyAttributeValue(focusedApp, kAXFocusedUIElementAttribute,
                                        (CFTypeRef *)&focusedEl);
    if (err != kAXErrorSuccess || !focusedEl) {
        CFRelease(focusedApp);
        CFRelease(sysWide);
        return fallback;
    }

    NSArray<NSScreen *> *screens = [NSScreen screens];
    if (screens.count == 0) {
        CFRelease(focusedEl);
        CFRelease(focusedApp);
        CFRelease(sysWide);
        return fallback;
    }
    CGFloat screenH = screens.firstObject.frame.size.height;

    // Try: bounds for the selected text range (works in most Cocoa text views).
    CFTypeRef rangeRef = NULL;
    if (AXUIElementCopyAttributeValue(focusedEl, kAXSelectedTextRangeAttribute,
                                      &rangeRef) == kAXErrorSuccess && rangeRef) {
        CFTypeRef boundsRef = NULL;
        if (AXUIElementCopyParameterizedAttributeValue(
                focusedEl, kAXBoundsForRangeParameterizedAttribute,
                rangeRef, &boundsRef) == kAXErrorSuccess && boundsRef) {

            CGRect axRect;
            if (AXValueGetValue(boundsRef, kAXValueTypeCGRect, &axRect) &&
                (axRect.size.width > 0 || axRect.size.height > 0 ||
                 axRect.origin.x > 0   || axRect.origin.y > 0)) {
                // AX coords: origin top-left.  NS coords: origin bottom-left.
                CGFloat nsX = axRect.origin.x;
                CGFloat nsY = screenH - axRect.origin.y;  // top of caret in NS
                CFRelease(boundsRef);
                CFRelease(rangeRef);
                CFRelease(focusedEl);
                CFRelease(focusedApp);
                CFRelease(sysWide);
                return NSMakePoint(nsX, nsY);
            }
            CFRelease(boundsRef);
        }
        CFRelease(rangeRef);
    }

    // Fallback: use the position of the focused UI element itself.
    CFTypeRef posRef = NULL;
    if (AXUIElementCopyAttributeValue(focusedEl, kAXPositionAttribute,
                                      (CFTypeRef *)&posRef) == kAXErrorSuccess && posRef) {
        CGPoint elPos;
        if (AXValueGetValue(posRef, kAXValueTypeCGPoint, &elPos)) {
            CFRelease(posRef);
            CFRelease(focusedEl);
            CFRelease(focusedApp);
            CFRelease(sysWide);
            return NSMakePoint(elPos.x + 20, screenH - elPos.y);
        }
        CFRelease(posRef);
    }

    CFRelease(focusedEl);
    CFRelease(focusedApp);
    CFRelease(sysWide);
    return fallback;
}

// ── Create (once) ────────────────────────────────────────────────────

static void ensureOverlay(void) {
    if (ovPanel) return;

    ovPanel = [[NSPanel alloc]
        initWithContentRect:NSMakeRect(0, 0, OV_REC_W, OV_REC_H)
                  styleMask:NSWindowStyleMaskBorderless | NSWindowStyleMaskNonactivatingPanel
                    backing:NSBackingStoreBuffered
                      defer:YES];
    ovPanel.level                = NSFloatingWindowLevel;
    ovPanel.opaque               = NO;
    ovPanel.backgroundColor      = [NSColor clearColor];
    ovPanel.hasShadow            = YES;
    ovPanel.movableByWindowBackground = NO;
    ovPanel.hidesOnDeactivate    = NO;
    ovPanel.ignoresMouseEvents   = YES;
    ovPanel.collectionBehavior   = NSWindowCollectionBehaviorCanJoinAllSpaces
                                 | NSWindowCollectionBehaviorTransient;

    NSRect initBounds = [[ovPanel contentView] bounds];

    // ── Recording container (waveform) ──
    recContainer = [[NSView alloc] initWithFrame:initBounds];
    recContainer.wantsLayer          = YES;
    recContainer.layer.cornerRadius  = OV_RADIUS;
    recContainer.layer.masksToBounds = YES;

    NSVisualEffectView *recBlur = [[NSVisualEffectView alloc] initWithFrame:recContainer.bounds];
    if (@available(macOS 10.14, *)) {
        recBlur.material      = NSVisualEffectMaterialHUDWindow;
    } else {
        recBlur.material      = NSVisualEffectMaterialDark;
    }
    recBlur.state             = NSVisualEffectStateActive;
    recBlur.blendingMode      = NSVisualEffectBlendingModeBehindWindow;
    recBlur.appearance        = [NSAppearance appearanceNamed:NSAppearanceNameVibrantDark];
    recBlur.autoresizingMask  = NSViewWidthSizable | NSViewHeightSizable;
    [recContainer addSubview:recBlur];

    waveView = [[DictationWaveformView alloc] initWithFrame:recContainer.bounds];
    waveView.autoresizingMask = NSViewWidthSizable | NSViewHeightSizable;
    [recContainer addSubview:waveView];

    // ── Thinking container (spinner + label) ──
    thinkContainer = [[NSView alloc] initWithFrame:initBounds];
    thinkContainer.wantsLayer          = YES;
    thinkContainer.layer.cornerRadius  = OV_RADIUS;
    thinkContainer.layer.masksToBounds = YES;

    NSVisualEffectView *thinkBlur = [[NSVisualEffectView alloc] initWithFrame:thinkContainer.bounds];
    if (@available(macOS 10.14, *)) {
        thinkBlur.material      = NSVisualEffectMaterialHUDWindow;
    } else {
        thinkBlur.material      = NSVisualEffectMaterialDark;
    }
    thinkBlur.state             = NSVisualEffectStateActive;
    thinkBlur.blendingMode      = NSVisualEffectBlendingModeBehindWindow;
    thinkBlur.appearance        = [NSAppearance appearanceNamed:NSAppearanceNameVibrantDark];
    thinkBlur.autoresizingMask  = NSViewWidthSizable | NSViewHeightSizable;
    [thinkContainer addSubview:thinkBlur];

    thinkSpinner = [[NSProgressIndicator alloc] initWithFrame:NSMakeRect(14, 10, 16, 16)];
    thinkSpinner.style                = NSProgressIndicatorStyleSpinning;
    thinkSpinner.controlSize          = NSControlSizeSmall;
    thinkSpinner.displayedWhenStopped = NO;
    thinkSpinner.wantsLayer           = YES;
    thinkSpinner.appearance           = [NSAppearance appearanceNamed:NSAppearanceNameVibrantDark];
    [thinkContainer addSubview:thinkSpinner];

    thinkLabel = [[NSTextField alloc] initWithFrame:NSMakeRect(38, 9, 96, 18)];
    thinkLabel.stringValue      = @"Thinking";
    thinkLabel.font             = [NSFont systemFontOfSize:13 weight:NSFontWeightMedium];
    thinkLabel.textColor        = [NSColor whiteColor];
    thinkLabel.backgroundColor  = [NSColor clearColor];
    thinkLabel.bezeled           = NO;
    thinkLabel.editable          = NO;
    thinkLabel.selectable        = NO;
    thinkLabel.drawsBackground   = NO;
    [thinkContainer addSubview:thinkLabel];

    // Configure both containers to auto-resize with parent window bounds
    recContainer.autoresizingMask = NSViewWidthSizable | NSViewHeightSizable;
    thinkContainer.autoresizingMask = NSViewWidthSizable | NSViewHeightSizable;

    // Set initial alphas to hidden
    recContainer.alphaValue = 0.0;
    thinkContainer.alphaValue = 0.0;

    // Add both containers to the window's contentView immediately
    [[ovPanel contentView] addSubview:recContainer];
    [[ovPanel contentView] addSubview:thinkContainer];
}

// ── Position helper ──────────────────────────────────────────────────

static NSRect calculateOverlayFrame(CGFloat panelW, CGFloat panelH) {
    NSPoint caret = getCaretPosition();
    CGFloat x = caret.x - panelW / 2.0;
    CGFloat y = caret.y + OV_GAP_Y;

    // Clamp to visible screen area.
    NSScreen *scr = [NSScreen mainScreen];
    if (!scr) scr = [[NSScreen screens] firstObject];
    if (!scr) { return NSMakeRect(x, y, panelW, panelH); }
    NSRect sf = scr.frame;
    if (x < sf.origin.x + 4)           x = sf.origin.x + 4;
    if (x + panelW > NSMaxX(sf) - 4)   x = NSMaxX(sf) - panelW - 4;
    if (y + panelH > NSMaxY(sf) - 4)   y = caret.y - panelH - OV_GAP_Y; // flip below
    if (y < sf.origin.y + 4)           y = sf.origin.y + 4;

    return NSMakeRect(x, y, panelW, panelH);
}

// ═══════════════════════════════════════════════════════════════════════
// Public C API (called from dictation_darwin.go via CGO)
// ═══════════════════════════════════════════════════════════════════════

// Pre-create the overlay panel and all subviews so the first press is instant.
void PreCreateDictationOverlay(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        ensureOverlay();
    });
}

// state: 1 = recording, 2 = thinking
void ShowDictationOverlay(int state, const char *labelText) {
    NSString *label = labelText ? [NSString stringWithUTF8String:labelText] : @"Thinking";
    dispatch_async(dispatch_get_main_queue(), ^{
        ensureOverlay();

        // Check if panel is already visible and faded in
        BOOL wasVisible = ovPanel.isVisible && (ovPanel.alphaValue > 0.1);

        // Stop current animation timers first so they don't leak or conflict
        [waveView stopAnimating];
        [thinkSpinner stopAnimation:nil];
        if (dotsTimer) { [dotsTimer invalidate]; dotsTimer = nil; }

        CGFloat panelW = OV_REC_W;
        CGFloat panelH = OV_REC_H;

        if (state == 1) {
            panelW = OV_REC_W;
            panelH = OV_REC_H;
        } else if (state == 2) {
            NSDictionary *attrs = @{ NSFontAttributeName : thinkLabel.font };
            NSSize txtSize = [label sizeWithAttributes:attrs];
            // Symmetric padding: Left margin (14) + spinner (16) + gap (8) + text + Right margin (14)
            panelW = 14 + 16 + 8 + txtSize.width + 14;
            if (panelW < OV_THINK_W) panelW = OV_THINK_W;
            panelH = OV_THINK_H;
        }

        NSRect targetFrame = calculateOverlayFrame(panelW, panelH);

        // Set up the state-specific configurations
        if (state == 1) {
            [waveView startAnimating];
        } else if (state == 2) {
            // Update think label text and layout
            thinkLabel.stringValue = label;
            thinkLabel.frame = NSMakeRect(38, 9, panelW - 38 - 14, 18);
            [thinkSpinner startAnimation:nil];

            dotsCount = 0;
            dotsTimer = [NSTimer scheduledTimerWithTimeInterval:0.4 repeats:YES
                                                         block:^(NSTimer * _Nonnull t) {
                dotsCount = (dotsCount + 1) % 4;
                NSString *dots = [@"" stringByPaddingToLength:dotsCount
                                                  withString:@"." startingAtIndex:0];
                thinkLabel.stringValue = [NSString stringWithFormat:@"%@%@", label, dots];
            }];
        } else {
            [ovPanel orderOut:nil];
            return;
        }

        if (wasVisible) {
            // Smoothly animate frame, and fade containers in/out
            [NSAnimationContext runAnimationGroup:^(NSAnimationContext *ctx) {
                ctx.duration = 0.22;
                ctx.timingFunction = [CAMediaTimingFunction functionWithName:kCAMediaTimingFunctionEaseInEaseOut];
                
                [[ovPanel animator] setFrame:targetFrame display:YES];
                
                if (state == 1) {
                    [[recContainer animator] setAlphaValue:1.0];
                    [[thinkContainer animator] setAlphaValue:0.0];
                } else if (state == 2) {
                    [[recContainer animator] setAlphaValue:0.0];
                    [[thinkContainer animator] setAlphaValue:1.0];
                }
            } completionHandler:nil];
        } else {
            // Position panel instantly, set initial opacity of subviews
            [ovPanel setFrame:targetFrame display:YES];
            
            if (state == 1) {
                recContainer.alphaValue = 1.0;
                thinkContainer.alphaValue = 0.0;
            } else if (state == 2) {
                recContainer.alphaValue = 0.0;
                thinkContainer.alphaValue = 1.0;
            }
            
            // Fade in the panel from 0 to 1
            ovPanel.alphaValue = 0.0;
            [ovPanel orderFront:nil];
            [NSAnimationContext runAnimationGroup:^(NSAnimationContext *ctx) {
                ctx.duration = 0.15;
                ovPanel.animator.alphaValue = 1.0;
            } completionHandler:nil];
        }
    });
}

void HideDictationOverlay(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (!ovPanel || !ovPanel.isVisible) return;

        [waveView stopAnimating];
        [thinkSpinner stopAnimation:nil];
        if (dotsTimer) { [dotsTimer invalidate]; dotsTimer = nil; }

        [NSAnimationContext runAnimationGroup:^(NSAnimationContext *ctx) {
            ctx.duration = 0.12;
            ovPanel.animator.alphaValue = 0;
        } completionHandler:^{
            [ovPanel orderOut:nil];
        }];
    });
}
