// +build darwin

#import <Speech/Speech.h>
#import <AVFoundation/AVFoundation.h>

// ─── Go-exported callback functions (defined in recognizer_darwin.go) ───
extern void goSpeechResult(const char *text, int isFinal);
extern void goSpeechError(const char *error);
extern void goRecordingError(const char *error);

// ═══════════════════════════════════════════════════════════════════════
// Native Speech Recognition (Apple Speech framework — legacy)
// ═══════════════════════════════════════════════════════════════════════

static AVAudioEngine *audioEngine = nil;
static id recognitionRequest = nil;  // SFSpeechAudioBufferRecognitionRequest (10.15+)
static id recognitionTask = nil;     // SFSpeechRecognitionTask (10.15+)

void StartSpeechRecognition(const char *locale) {
    if (@available(macOS 10.15, *)) {
        // Stop any previous session first.
        if (audioEngine && audioEngine.isRunning) {
            [audioEngine stop];
            [audioEngine.inputNode removeTapOnBus:0];
        }
        if (recognitionRequest) {
            [(SFSpeechAudioBufferRecognitionRequest *)recognitionRequest endAudio];
            recognitionRequest = nil;
        }
        if (recognitionTask) {
            [(SFSpeechRecognitionTask *)recognitionTask cancel];
            recognitionTask = nil;
        }

        [SFSpeechRecognizer requestAuthorization:^(SFSpeechRecognizerAuthorizationStatus status) {
            if (status != SFSpeechRecognizerAuthorizationStatusAuthorized) {
                const char *msg;
                switch (status) {
                    case SFSpeechRecognizerAuthorizationStatusDenied:
                        msg = "Speech recognition permission denied. Enable it in System Settings > Privacy & Security > Speech Recognition.";
                        break;
                    case SFSpeechRecognizerAuthorizationStatusRestricted:
                        msg = "Speech recognition is restricted on this device.";
                        break;
                    default:
                        msg = "Speech recognition authorization failed.";
                        break;
                }
                goSpeechError(msg);
                return;
            }

            dispatch_async(dispatch_get_main_queue(), ^{
                NSString *localeStr = [NSString stringWithUTF8String:locale];
                SFSpeechRecognizer *recognizer = [[SFSpeechRecognizer alloc]
                    initWithLocale:[NSLocale localeWithLocaleIdentifier:localeStr]];

                if (!recognizer || !recognizer.isAvailable) {
                    goSpeechError("Speech recognizer is not available for this language.");
                    return;
                }

                audioEngine = [[AVAudioEngine alloc] init];
                SFSpeechAudioBufferRecognitionRequest *req =
                    [[SFSpeechAudioBufferRecognitionRequest alloc] init];
                req.shouldReportPartialResults = YES;
                recognitionRequest = req;

                recognitionTask = [recognizer recognitionTaskWithRequest:req
                    resultHandler:^(SFSpeechRecognitionResult *result, NSError *error) {
                        if (result) {
                            NSString *text = result.bestTranscription.formattedString;
                            goSpeechResult([text UTF8String], result.isFinal ? 1 : 0);
                        }
                        if (error) {
                            // Ignore cancellation errors (code 216 = cancelled, code 1 = generic cancel).
                            if (error.code != 216 && error.code != 1) {
                                goSpeechError([[error localizedDescription] UTF8String]);
                            }
                        }
                    }];

                AVAudioInputNode *inputNode = audioEngine.inputNode;
                AVAudioFormat *format = [inputNode outputFormatForBus:0];
                [inputNode installTapOnBus:0 bufferSize:1024 format:format
                    block:^(AVAudioPCMBuffer *buffer, AVAudioTime *when) {
                        [req appendAudioPCMBuffer:buffer];
                    }];

                [audioEngine prepare];
                NSError *engineError = nil;
                if (![audioEngine startAndReturnError:&engineError]) {
                    NSString *msg = [NSString stringWithFormat:@"Audio engine failed: %@",
                        [engineError localizedDescription]];
                    goSpeechError([msg UTF8String]);
                    return;
                }
            });
        }];
    } else {
        goSpeechError("Speech recognition requires macOS 10.15 (Catalina) or later.");
    }
}

void StopSpeechRecognition(void) {
    if (@available(macOS 10.15, *)) {
        dispatch_async(dispatch_get_main_queue(), ^{
            if (recognitionRequest) {
                [(SFSpeechAudioBufferRecognitionRequest *)recognitionRequest endAudio];
                recognitionRequest = nil;
            }
            if (audioEngine && audioEngine.isRunning) {
                [audioEngine stop];
                [audioEngine.inputNode removeTapOnBus:0];
            }
            if (recognitionTask) {
                [(SFSpeechRecognitionTask *)recognitionTask cancel];
                recognitionTask = nil;
            }
        });
    }
}

// ═══════════════════════════════════════════════════════════════════════
// Audio Recording (for API-based transcription via OpenAI / Deepgram)
// Records microphone audio to an m4a file using AVAudioRecorder.
// ═══════════════════════════════════════════════════════════════════════

static AVAudioRecorder *apiRecorder = nil;

// Forward declarations.
void StopAudioRecording(void);
int  IsAudioRecording(void);

// Internal helper — actually starts AVAudioRecorder on the main thread.
static void doStartRecording(NSString *path) {
    NSURL *url = [NSURL fileURLWithPath:path];

    // Record as m4a (AAC) — compact file size, supported by OpenAI API.
    NSDictionary *settings = @{
        AVFormatIDKey:            @(kAudioFormatMPEG4AAC),
        AVSampleRateKey:          @(16000),
        AVNumberOfChannelsKey:    @(1),
        AVEncoderAudioQualityKey: @(AVAudioQualityHigh),
    };

    NSError *error = nil;
    apiRecorder = [[AVAudioRecorder alloc] initWithURL:url
                                              settings:settings
                                                 error:&error];
    if (!apiRecorder) {
        const char *msg = error
            ? [[error localizedDescription] UTF8String]
            : "Failed to create audio recorder";
        goRecordingError(msg);
        return;
    }

    if (![apiRecorder record]) {
        goRecordingError("Failed to start audio recording");
        apiRecorder = nil;
    }
}

// Internal helper — stops any active recording inline (must be called on main thread).
static void stopRecorderInline(void) {
    if (apiRecorder) {
        if (apiRecorder.isRecording) {
            [apiRecorder stop];
        }
        apiRecorder = nil;
    }
}

void StartAudioRecording(const char *outputPath) {
    // NOTE: We intentionally do NOT call StopAudioRecording() here because
    // it uses dispatch_sync(main_queue), which forces the main thread to
    // drain all pending async blocks (overlay, menu bar, sound) before
    // the sync block runs — and if any of those blocks crash, the app dies.
    // Instead, we inline the stop inside the same dispatch_async block as
    // the start, guaranteeing correct ordering without blocking.

    NSString *path = [NSString stringWithUTF8String:outputPath];

    // Check and request microphone permission.
    if (@available(macOS 10.14, *)) {
        AVAuthorizationStatus status =
            [AVCaptureDevice authorizationStatusForMediaType:AVMediaTypeAudio];

        if (status == AVAuthorizationStatusDenied ||
            status == AVAuthorizationStatusRestricted) {
            goRecordingError("Microphone permission denied. Enable it in "
                "System Settings → Privacy & Security → Microphone.");
            return;
        }

        if (status == AVAuthorizationStatusNotDetermined) {
            // First-time request — show macOS permission dialog.
            [AVCaptureDevice requestAccessForMediaType:AVMediaTypeAudio
                completionHandler:^(BOOL granted) {
                    if (granted) {
                        dispatch_async(dispatch_get_main_queue(), ^{
                            stopRecorderInline();
                            doStartRecording(path);
                        });
                    } else {
                        goRecordingError("Microphone permission denied.");
                    }
                }];
            return;
        }
    }

    // Permission already granted — stop old & start new on main thread.
    dispatch_async(dispatch_get_main_queue(), ^{
        stopRecorderInline();
        doStartRecording(path);
    });
}

void StopAudioRecording(void) {
    // Use dispatch_sync so the caller can safely read the file afterwards.
    dispatch_block_t stopBlock = ^{
        if (apiRecorder) {
            if (apiRecorder.isRecording) {
                [apiRecorder stop];
            }
            apiRecorder = nil;
        }
    };

    if ([NSThread isMainThread]) {
        stopBlock();
    } else {
        dispatch_sync(dispatch_get_main_queue(), stopBlock);
    }
}

int IsAudioRecording(void) {
    return (apiRecorder && apiRecorder.isRecording) ? 1 : 0;
}
