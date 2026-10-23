import AVFoundation
import Combine
import Foundation
import Speech

@MainActor
final class SpeechController: NSObject, ObservableObject, AVSpeechSynthesizerDelegate {
    @Published private(set) var listening = false
    @Published private(set) var starting = false
    @Published var transcript = ""
    @Published var message = ""

    private let engine = AVAudioEngine()
    private let synthesizer = AVSpeechSynthesizer()
    private var request: SFSpeechAudioBufferRecognitionRequest?
    private var task: SFSpeechRecognitionTask?
    private var tapInstalled = false

    override init() {
        super.init()
        synthesizer.delegate = self
    }

    func speak(_ text: String) {
        guard !text.isEmpty else { return }
        synthesizer.stopSpeaking(at: .immediate)
        let utterance = AVSpeechUtterance(string: text)
        utterance.rate = 0.5
        synthesizer.speak(utterance)
    }

    func start() async {
        if listening { stop(); return }
        guard !starting else { return }
        starting = true
        defer { starting = false }
        synthesizer.stopSpeaking(at: .immediate)
        let speechPermission = await withCheckedContinuation { continuation in
            SFSpeechRecognizer.requestAuthorization { continuation.resume(returning: $0) }
        }
        guard speechPermission == .authorized else {
            message = "Speech recognition permission is off. You can type instead."
            return
        }
        let microphonePermission = await withCheckedContinuation { continuation in
            AVAudioSession.sharedInstance().requestRecordPermission { continuation.resume(returning: $0) }
        }
        guard microphonePermission else {
            message = "Microphone permission is off. You can type instead."
            return
        }
        guard let recognizer = SFSpeechRecognizer(locale: Locale.current), recognizer.isAvailable else {
            message = "Speech recognition is unavailable for this language right now. You can type instead."
            return
        }
        do {
            let session = AVAudioSession.sharedInstance()
            try session.setCategory(.playAndRecord, mode: .default, options: [.defaultToSpeaker, .duckOthers])
            try session.setActive(true, options: .notifyOthersOnDeactivation)
            let input = engine.inputNode
            let format = input.outputFormat(forBus: 0)
            guard session.isInputAvailable, format.sampleRate > 0, format.channelCount > 0 else {
                #if targetEnvironment(simulator)
                message = "No microphone input is available in Simulator. Connect an input device to this Mac, or type instead."
                #else
                message = "No microphone input is available. Check the audio input, or type instead."
                #endif
                try? session.setActive(false, options: .notifyOthersOnDeactivation)
                return
            }
            let request = SFSpeechAudioBufferRecognitionRequest()
            request.shouldReportPartialResults = true
            request.requiresOnDeviceRecognition = recognizer.supportsOnDeviceRecognition
            transcript = ""
            message = "Listening… tap Done when you finish."
            input.installTap(onBus: 0, bufferSize: 1024, format: format) { buffer, _ in
                request.append(buffer)
            }
            tapInstalled = true
            self.request = request
            engine.prepare()
            try engine.start()
            listening = true
            task = recognizer.recognitionTask(with: request) { [weak self] result, error in
                Task { @MainActor in
                    guard let self else { return }
                    if let result { self.transcript = result.bestTranscription.formattedString }
                    if let error, self.listening {
                        self.stop()
                        self.message = "Speech recognition stopped: \(error.localizedDescription). Review any captured text, or type instead."
                    } else if result?.isFinal == true {
                        self.stop()
                    }
                }
            }
        } catch {
            message = "Could not start the microphone: \(error.localizedDescription). You can type instead."
            stop()
        }
    }

    func stop() {
        guard request != nil || listening || tapInstalled else { return }
        if engine.isRunning { engine.stop() }
        if tapInstalled {
            engine.inputNode.removeTap(onBus: 0)
            tapInstalled = false
        }
        request?.endAudio()
        task?.cancel()
        task = nil
        request = nil
        listening = false
        try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)
        if !transcript.isEmpty { message = "Transcribed. Review the text, then send it." }
    }
}
