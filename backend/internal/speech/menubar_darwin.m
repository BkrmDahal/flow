// +build darwin

#import <Cocoa/Cocoa.h>
#import <ApplicationServices/ApplicationServices.h>
#import <AudioToolbox/AudioServices.h>

// Go-exported callbacks (defined in dictation_darwin.go).
extern void goDictationPressed(void);
extern void goDictationReleased(void);

// ═══════════════════════════════════════════════════════════════════════
// Menu Bar Status Item (NSStatusItem)
// ═══════════════════════════════════════════════════════════════════════

static NSStatusItem *statusItem  = nil;
static NSImage      *iconIdle    = nil;
static NSMenu       *statusMenu  = nil;
static NSMenuItem   *hotkeyMenuItem  = nil;
static NSMenuItem   *grammarHotkeyMenuItem = nil;
static NSMenuItem   *statusMenuItem  = nil;

@interface FlowMenuHandler : NSObject
- (void)quitApp:(id)sender;
- (void)showApp:(id)sender;
@end

@implementation FlowMenuHandler
- (void)quitApp:(id)sender {
    [[NSApplication sharedApplication] terminate:nil];
}
- (void)showApp:(id)sender {
    [NSApp activateIgnoringOtherApps:YES];
    for (NSWindow *w in [NSApp windows]) {
        if ([w isKindOfClass:[NSWindow class]] && w.isVisible) {
            [w makeKeyAndOrderFront:nil];
            break;
        }
    }
}
@end

static FlowMenuHandler *menuHandler = nil;

static NSImage* createMenuBarIcon(void) {
    CGFloat size = 18.0;
    NSImage *icon = [[NSImage alloc] initWithSize:NSMakeSize(size, size)];
    [icon lockFocus];

    [[NSColor blackColor] setStroke];

    NSBezierPath *path = [NSBezierPath bezierPath];
    [path setLineWidth:1.6];
    [path setLineCapStyle:NSRoundLineCapStyle];
    [path setLineJoinStyle:NSRoundLineJoinStyle];

    // Start at top-left lobe (5.0, 14.5)
    [path moveToPoint:NSMakePoint(5.0, 14.5)];
    
    // Left Lobe: Curve down-left to far-left (2.0) and down-right to bottom-left (5.0, 3.5)
    [path curveToPoint:NSMakePoint(5.0, 3.5)
          controlPoint1:NSMakePoint(2.0, 13.5)
          controlPoint2:NSMakePoint(2.0, 4.5)];
          
    // Crossing 1: Diagonal fluid S-curve from bottom-left to top-right (13.0, 14.5)
    [path curveToPoint:NSMakePoint(13.0, 14.5)
          controlPoint1:NSMakePoint(7.0, 5.5)
          controlPoint2:NSMakePoint(11.0, 12.5)];
          
    // Right Lobe: Curve down-right to far-right (16.0) and down-left to bottom-right (13.0, 3.5)
    [path curveToPoint:NSMakePoint(13.0, 3.5)
          controlPoint1:NSMakePoint(16.0, 13.5)
          controlPoint2:NSMakePoint(16.0, 4.5)];
          
    // Crossing 2: Diagonal fluid S-curve from bottom-right to top-left (5.0, 14.5)
    [path curveToPoint:NSMakePoint(5.0, 14.5)
          controlPoint1:NSMakePoint(11.0, 5.5)
          controlPoint2:NSMakePoint(7.0, 12.5)];

    [path stroke];

    [icon unlockFocus];
    [icon setTemplate:YES];
    return icon;
}

void FlowShowMenuBar(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (statusItem) return;

        iconIdle = createMenuBarIcon();

        if (!menuHandler) {
            menuHandler = [[FlowMenuHandler alloc] init];
        }

        statusItem = [[NSStatusBar systemStatusBar]
            statusItemWithLength:NSVariableStatusItemLength];
        statusItem.button.image   = iconIdle;
        statusItem.button.toolTip = @"Flow";

        statusMenu = [[NSMenu alloc] init];

        statusMenuItem = [[NSMenuItem alloc]
            initWithTitle:@"Flow"
            action:nil keyEquivalent:@""];
        [statusMenuItem setEnabled:NO];
        {
            NSImage *menuIcon = createMenuBarIcon();
            if (menuIcon) {
                [menuIcon setSize:NSMakeSize(16, 16)];
                [statusMenuItem setImage:menuIcon];
            }
        }
        [statusMenu addItem:statusMenuItem];
        [statusMenu addItem:[NSMenuItem separatorItem]];

        hotkeyMenuItem = [[NSMenuItem alloc]
            initWithTitle:@"Hotkey: not configured"
            action:nil keyEquivalent:@""];
        [hotkeyMenuItem setEnabled:NO];
        [statusMenu addItem:hotkeyMenuItem];

        grammarHotkeyMenuItem = [[NSMenuItem alloc]
            initWithTitle:@"Double-tap to fix grammar"
            action:nil keyEquivalent:@""];
        [grammarHotkeyMenuItem setEnabled:NO];
        [statusMenu addItem:grammarHotkeyMenuItem];
        [statusMenu addItem:[NSMenuItem separatorItem]];

        NSMenuItem *showItem = [[NSMenuItem alloc]
            initWithTitle:@"Show Flow"
            action:@selector(showApp:) keyEquivalent:@""];
        [showItem setTarget:menuHandler];
        [statusMenu addItem:showItem];
        [statusMenu addItem:[NSMenuItem separatorItem]];

        NSMenuItem *quitItem = [[NSMenuItem alloc]
            initWithTitle:@"Quit Flow"
            action:@selector(quitApp:) keyEquivalent:@"q"];
        [quitItem setTarget:menuHandler];
        [statusMenu addItem:quitItem];

        statusItem.menu = statusMenu;
    });
}

void FlowHideMenuBar(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (statusItem) {
            [[NSStatusBar systemStatusBar] removeStatusItem:statusItem];
            statusItem = nil;
            statusMenu = nil;
            hotkeyMenuItem = nil;
            grammarHotkeyMenuItem = nil;
            statusMenuItem = nil;
        }
    });
}

void FlowSetMenuBarHotkeyLabel(const char *label) {
    NSString *str = [NSString stringWithUTF8String:label];
    dispatch_async(dispatch_get_main_queue(), ^{
        if (hotkeyMenuItem) {
            [hotkeyMenuItem setTitle:str];
        }
    });
}

void FlowSetMenuBarGrammarHotkeyLabel(const char *label) {
    NSString *str = [NSString stringWithUTF8String:label];
    dispatch_async(dispatch_get_main_queue(), ^{
        if (grammarHotkeyMenuItem) {
            [grammarHotkeyMenuItem setTitle:str];
        }
    });
}

// state: 0 = idle, 1 = recording, 2 = transcribing
void FlowSetMenuBarState(int state) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (!statusItem) return;
        switch (state) {
            case 1:
                statusItem.button.toolTip = @"Flow — Recording…";
                if (statusMenuItem) [statusMenuItem setTitle:@"Recording…"];
                break;
            case 2:
                statusItem.button.toolTip = @"Flow — Transcribing…";
                if (statusMenuItem) [statusMenuItem setTitle:@"Transcribing…"];
                break;
            default:
                statusItem.button.toolTip = @"Flow";
                if (statusMenuItem) [statusMenuItem setTitle:@"Flow"];
                break;
        }
    });
}

// ═══════════════════════════════════════════════════════════════════════
// Global Modifier-Key Push-to-Talk Monitor
// ═══════════════════════════════════════════════════════════════════════

#define DEV_LCTRL   0x00000001
#define DEV_LSHIFT  0x00000002
#define DEV_RSHIFT  0x00000004
#define DEV_LCMD    0x00000008
#define DEV_RCMD    0x00000010
#define DEV_LALT    0x00000020
#define DEV_RALT    0x00000040
#define DEV_RCTRL   0x00002000

static id  globalMonitor = nil;
static id  localMonitor  = nil;
static uint64_t hotkeyMask = DEV_LALT;
static BOOL isHotkeyDown  = NO;

static void handleModifierEvent(NSEvent *event) {
    CGEventRef cgEvt = event.CGEvent;
    if (!cgEvt) return;

    CGEventFlags flags = CGEventGetFlags(cgEvt);
    BOOL keyIsDown = (flags & hotkeyMask) != 0;

    if (keyIsDown && !isHotkeyDown) {
        isHotkeyDown = YES;
        goDictationPressed();
    } else if (!keyIsDown && isHotkeyDown) {
        isHotkeyDown = NO;
        goDictationReleased();
    }
}

void FlowSetHotkeyModifier(int keyCode) {
    switch (keyCode) {
        case 0: hotkeyMask = DEV_LALT;  break;
        case 1: hotkeyMask = DEV_RALT;  break;
        case 2: hotkeyMask = DEV_LCMD;  break;
        case 3: hotkeyMask = DEV_RCMD;  break;
        case 4: hotkeyMask = DEV_LCTRL; break;
        case 5: hotkeyMask = DEV_RCTRL; break;
        default: hotkeyMask = DEV_LALT; break;
    }
}

void FlowStartHotkeyMonitor(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (globalMonitor) return;

        globalMonitor = [NSEvent
            addGlobalMonitorForEventsMatchingMask:NSEventMaskFlagsChanged
            handler:^(NSEvent *event) {
                handleModifierEvent(event);
            }];

        localMonitor = [NSEvent
            addLocalMonitorForEventsMatchingMask:NSEventMaskFlagsChanged
            handler:^NSEvent *(NSEvent *event) {
                handleModifierEvent(event);
                return event;
            }];

        NSLog(@"[flow] hotkey monitor started");
    });
}

void FlowStopHotkeyMonitor(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (globalMonitor) {
            [NSEvent removeMonitor:globalMonitor];
            globalMonitor = nil;
        }
        if (localMonitor) {
            [NSEvent removeMonitor:localMonitor];
            localMonitor = nil;
        }
        isHotkeyDown = NO;
        NSLog(@"[flow] hotkey monitor stopped");
    });
}

// ═══════════════════════════════════════════════════════════════════════
// Text Injection — paste text into the focused application
// ═══════════════════════════════════════════════════════════════════════

void FlowTypeTextViaClipboard(const char *text) {
    NSString *str = [NSString stringWithUTF8String:text];
    NSPasteboard *pb = [NSPasteboard generalPasteboard];

    NSString *savedText = [[pb stringForType:NSPasteboardTypeString] copy];

    [pb clearContents];
    [pb setString:str forType:NSPasteboardTypeString];

    usleep(50000); // 50 ms

    CGEventSourceRef source = CGEventSourceCreate(kCGEventSourceStateHIDSystemState);

    CGEventRef keyDown = CGEventCreateKeyboardEvent(source, (CGKeyCode)9, true);
    CGEventSetFlags(keyDown, kCGEventFlagMaskCommand);

    CGEventRef keyUp = CGEventCreateKeyboardEvent(source, (CGKeyCode)9, false);
    CGEventSetFlags(keyUp, kCGEventFlagMaskCommand);

    CGEventPost(kCGHIDEventTap, keyDown);
    CGEventPost(kCGHIDEventTap, keyUp);

    CFRelease(keyDown);
    CFRelease(keyUp);
    if (source) CFRelease(source);

    if (savedText) {
        NSString *textToRestore = savedText;
        dispatch_after(dispatch_time(DISPATCH_TIME_NOW, (int64_t)(1.0 * NSEC_PER_SEC)),
                       dispatch_get_main_queue(), ^{
            NSPasteboard *rpb = [NSPasteboard generalPasteboard];
            [rpb clearContents];
            [rpb setString:textToRestore forType:NSPasteboardTypeString];
        });
    }
}

// ═══════════════════════════════════════════════════════════════════════
// Focus Management
// ═══════════════════════════════════════════════════════════════════════

static NSRunningApplication *savedFrontApp = nil;
static pid_t savedFrontPid = 0;

void FlowSaveFocusedApp(void) {
    NSRunningApplication *app = [[NSWorkspace sharedWorkspace] frontmostApplication];
    if (app) {
        savedFrontApp = app;
        savedFrontPid = app.processIdentifier;
    }
}

void FlowRestoreFocusedApp(void) {
    if (savedFrontApp) {
        [savedFrontApp activateWithOptions:NSApplicationActivateIgnoringOtherApps];
        usleep(150000);
        savedFrontApp = nil;
        savedFrontPid = 0;
    }
}

// ═══════════════════════════════════════════════════════════════════════
// Text Extraction — read selected text from focused app
// ═══════════════════════════════════════════════════════════════════════

static NSString* getSelectedTextViaAX(void) {
    if (!AXIsProcessTrusted()) return nil;

    AXUIElementRef sysWide = AXUIElementCreateSystemWide();
    if (!sysWide) return nil;

    AXUIElementRef focusedApp = NULL;
    AXError err = AXUIElementCopyAttributeValue(sysWide, kAXFocusedApplicationAttribute,
                                                (CFTypeRef *)&focusedApp);
    if (err != kAXErrorSuccess || !focusedApp) {
        CFRelease(sysWide);
        return nil;
    }

    AXUIElementRef focusedEl = NULL;
    err = AXUIElementCopyAttributeValue(focusedApp, kAXFocusedUIElementAttribute,
                                        (CFTypeRef *)&focusedEl);
    if (err != kAXErrorSuccess || !focusedEl) {
        CFRelease(focusedApp);
        CFRelease(sysWide);
        return nil;
    }

    CFTypeRef selectedTextRef = NULL;
    err = AXUIElementCopyAttributeValue(focusedEl, kAXSelectedTextAttribute, &selectedTextRef);

    CFRelease(focusedEl);
    CFRelease(focusedApp);
    CFRelease(sysWide);

    if (err != kAXErrorSuccess || !selectedTextRef) return nil;

    NSString *text = (__bridge_transfer NSString *)selectedTextRef;
    return (text && text.length > 0) ? text : nil;
}

static NSString* getSelectedTextViaCmdC(void) {
    NSPasteboard *pb = [NSPasteboard generalPasteboard];
    NSString *savedText = [[pb stringForType:NSPasteboardTypeString] copy];

    [pb clearContents];
    NSInteger clearedChangeCount = [pb changeCount];
    
    // Give the pasteboard a tiny moment to register the clear
    usleep(20000);

    CGEventSourceRef source = CGEventSourceCreate(kCGEventSourceStateHIDSystemState);

    CGEventRef keyDown = CGEventCreateKeyboardEvent(source, (CGKeyCode)8, true);
    CGEventSetFlags(keyDown, kCGEventFlagMaskCommand);

    CGEventRef keyUp = CGEventCreateKeyboardEvent(source, (CGKeyCode)8, false);
    CGEventSetFlags(keyUp, kCGEventFlagMaskCommand);

    CGEventPost(kCGHIDEventTap, keyDown);
    CGEventPost(kCGHIDEventTap, keyUp);

    CFRelease(keyDown);
    CFRelease(keyUp);
    if (source) CFRelease(source);

    // Poll the pasteboard until the change count increments, up to 400ms.
    int maxWaitMs = 400;
    while ([pb changeCount] == clearedChangeCount && maxWaitMs > 0) {
        usleep(10000); // 10ms
        maxWaitMs -= 10;
    }

    NSString *selectedText = [pb stringForType:NSPasteboardTypeString];

    // Restore the old pasteboard content after we've read the copied text
    dispatch_after(dispatch_time(DISPATCH_TIME_NOW, (int64_t)(0.2 * NSEC_PER_SEC)), dispatch_get_main_queue(), ^{
        NSPasteboard *rpb = [NSPasteboard generalPasteboard];
        [rpb clearContents];
        if (savedText) {
            [rpb setString:savedText forType:NSPasteboardTypeString];
        }
    });

    return (selectedText && selectedText.length > 0) ? selectedText : nil;
}

char* FlowCopySelectedText(void) {
    NSString *text = getSelectedTextViaAX();
    if (!text) {
        text = getSelectedTextViaCmdC();
    }
    if (text && text.length > 0) {
        return strdup([text UTF8String]);
    }
    return NULL;
}

int FlowReplaceSelectedText(const char *newText) {
    NSString *replacement = [NSString stringWithUTF8String:newText];

    if (AXIsProcessTrusted()) {
        AXUIElementRef sysWide = AXUIElementCreateSystemWide();
        if (sysWide) {
            AXUIElementRef focusedApp = NULL;
            AXError err = AXUIElementCopyAttributeValue(sysWide, kAXFocusedApplicationAttribute,
                                                        (CFTypeRef *)&focusedApp);
            if (err == kAXErrorSuccess && focusedApp) {
                AXUIElementRef focusedEl = NULL;
                err = AXUIElementCopyAttributeValue(focusedApp, kAXFocusedUIElementAttribute,
                                                    (CFTypeRef *)&focusedEl);
                if (err == kAXErrorSuccess && focusedEl) {
                    err = AXUIElementSetAttributeValue(focusedEl, kAXSelectedTextAttribute,
                                                      (__bridge CFTypeRef)replacement);
                    CFRelease(focusedEl);
                    CFRelease(focusedApp);
                    CFRelease(sysWide);
                    if (err == kAXErrorSuccess) return 1;
                } else {
                    CFRelease(focusedApp);
                    CFRelease(sysWide);
                }
            } else {
                CFRelease(sysWide);
            }
        }
    }

    FlowTypeTextViaClipboard(newText);
    return 1;
}

// ═══════════════════════════════════════════════════════════════════════
// Accessibility Permission Check
// ═══════════════════════════════════════════════════════════════════════

int FlowCheckAccessibilityPermission(int promptUser) {
    NSDictionary *options = @{
        (__bridge NSString *)kAXTrustedCheckOptionPrompt: @(promptUser ? YES : NO)
    };
    return AXIsProcessTrustedWithOptions((__bridge CFDictionaryRef)options) ? 1 : 0;
}

// ═══════════════════════════════════════════════════════════════════════
// Audio Feedback
// ═══════════════════════════════════════════════════════════════════════

static SystemSoundID cachedSounds[4] = {0, 0, 0, 0};
static BOOL soundsCached = NO;

static NSString *SoundFilePath(int soundType) {
    switch (soundType) {
        case 0: return @"/System/Library/Sounds/Pop.aiff";
        case 1: return @"/System/Library/Sounds/Tink.aiff";
        case 2: return @"/System/Library/Sounds/Glass.aiff";
        case 3: return @"/System/Library/Sounds/Basso.aiff";
        default: return nil;
    }
}

void FlowWarmUpAudioSystem(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (soundsCached) return;
        for (int i = 0; i < 4; i++) {
            NSString *path = SoundFilePath(i);
            if (!path) continue;
            NSURL *url = [NSURL fileURLWithPath:path];
            if (!url || ![[NSFileManager defaultManager] fileExistsAtPath:path]) continue;
            OSStatus status = AudioServicesCreateSystemSoundID((__bridge CFURLRef)url, &cachedSounds[i]);
            if (status != kAudioServicesNoError) {
                cachedSounds[i] = 0;
            }
        }
        soundsCached = YES;
    });
}

void FlowPlayDictationSound(int soundType) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (soundType < 0 || soundType > 3) return;
        if (!soundsCached) {
            for (int i = 0; i < 4; i++) {
                NSString *path = SoundFilePath(i);
                if (!path) continue;
                NSURL *url = [NSURL fileURLWithPath:path];
                if (url && [[NSFileManager defaultManager] fileExistsAtPath:path]) {
                    AudioServicesCreateSystemSoundID((__bridge CFURLRef)url, &cachedSounds[i]);
                }
            }
            soundsCached = YES;
        }
        if (cachedSounds[soundType] != 0) {
            AudioServicesPlaySystemSound(cachedSounds[soundType]);
        }
    });
}
