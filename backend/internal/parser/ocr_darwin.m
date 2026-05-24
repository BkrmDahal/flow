//go:build darwin
#import <Vision/Vision.h>
#import <AppKit/AppKit.h>
#import <Foundation/Foundation.h>

const char* PerformVisionOCR(const char* base64Str) {
    @autoreleasepool {
        NSString *nsBase64 = [NSString stringWithUTF8String:base64Str];
        NSData *imageData = [[NSData alloc] initWithBase64EncodedString:nsBase64 options:NSDataBase64DecodingIgnoreUnknownCharacters];
        if (!imageData) {
            return strdup("{\"error\":\"invalid base64\"}");
        }
        
        VNImageRequestHandler *handler = [[VNImageRequestHandler alloc] initWithData:imageData options:@{}];
        VNRecognizeTextRequest *request = [[VNRecognizeTextRequest alloc] init];
        request.recognitionLevel = VNRequestTextRecognitionLevelAccurate;
        request.usesLanguageCorrection = YES;
        
        NSError *error = nil;
        BOOL success = [handler performRequests:@[request] error:&error];
        if (!success) {
            NSString *errDesc = [error localizedDescription] ?: @"Unknown Vision error";
            NSDictionary *errDict = @{@"error": errDesc};
            NSData *errData = [NSJSONSerialization dataWithJSONObject:errDict options:0 error:nil];
            NSString *errJson = [[NSString alloc] initWithData:errData encoding:NSUTF8StringEncoding];
            return strdup([errJson UTF8String]);
        }
        
        NSMutableArray *blocks = [NSMutableArray array];
        for (VNRecognizedTextObservation *observation in request.results) {
            VNRecognizedText *recognizedText = [[observation topCandidates:1] firstObject];
            if (!recognizedText) continue;
            
            CGRect bbox = observation.boundingBox;
            // Convert to top-left origin coordinates
            double top = 1.0 - (bbox.origin.y + bbox.size.height);
            double left = bbox.origin.x;
            double width = bbox.size.width;
            double height = bbox.size.height;
            
            NSDictionary *dict = @{
                @"text": recognizedText.string,
                @"top": @(top),
                @"left": @(left),
                @"width": @(width),
                @"height": @(height)
            };
            [blocks addObject:dict];
        }
        
        NSError *jsonError = nil;
        NSData *jsonData = [NSJSONSerialization dataWithJSONObject:blocks options:0 error:&jsonError];
        if (jsonError) {
            return strdup("{\"error\":\"json formatting error\"}");
        }
        NSString *jsonString = [[NSString alloc] initWithData:jsonData encoding:NSUTF8StringEncoding];
        return strdup([jsonString UTF8String]);
    }
}
