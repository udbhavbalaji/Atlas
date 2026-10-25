import AVFoundation
import Combine
import Foundation
import Speech

@MainActor
final class SpeechController: NSObject, ObservableObject, AVSpeechSynthesizerDelegate {
    @Published private(set) var listening = false
    @Published private(set) var starting = false
    @Published private(set) var speaking = false
    @Published var transcript = ""
    @Published var message = ""

    private let engine = AVAudioEngine()
    private let synthesizer = AVSpeechSynthesizer()
    private var activeUtterance: AVSpeechUtterance?
    private var request: SFSpeechAudioBufferRecognitionRequest?
    private var task: SFSpeechRecognitionTask?
    private var tapInstalled = false

    override init() {
        super.init()
        synthesizer.delegate = self
    }

    func speak(_ text: String) {
        stopSpeaking()
        guard !text.isEmpty else { return }
        let utterance = AVSpeechUtterance(string: spokenSummary(text))
        utterance.rate = 0.5
        activeUtterance = utterance
        speaking = true
        synthesizer.speak(utterance)
    }

    func stopSpeaking() {
        activeUtterance = nil
        speaking = false
        synthesizer.stopSpeaking(at: .immediate)
    }

    nonisolated func speechSynthesizer(_ synthesizer: AVSpeechSynthesizer, didFinish utterance: AVSpeechUtterance) {
        Task { @MainActor in
            if activeUtterance === utterance {
                activeUtterance = nil
                speaking = false
            }
        }
    }

    nonisolated func speechSynthesizer(_ synthesizer: AVSpeechSynthesizer, didCancel utterance: AVSpeechUtterance) {
        Task { @MainActor in
            if activeUtterance === utterance {
                activeUtterance = nil
                speaking = false
            }
        }
    }

    private func spokenSummary(_ text: String) -> String {
        let lines = text.split(separator: "\n").map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }
        guard lines.count > 1, lines[0].hasPrefix("Here are ") else { return text }
        guard let noun = ["tasks", "reminders", "notes"].first(where: { lines[0].hasSuffix($0 + ":") }) else { return text }
        let titles = lines.dropFirst().compactMap { line -> String? in
            guard line.hasPrefix("“"), let end = line.dropFirst().firstIndex(of: "”") else { return nil }
            return String(line[line.index(after: line.startIndex)..<end])
        }
        guard titles.count == lines.count - 1 else { return text }
        let qualifier = lines[0].contains("open " + noun) ? "open " : lines[0].contains("upcoming " + noun) ? "upcoming " : ""
        let count = titles.count
        let item = count == 1 ? String(noun.dropLast()) : noun
        let prefix = "I found \(count)\(count == 10 ? " or more" : "") \(qualifier)\(item). "
        let first = titles.prefix(3)
        let remainder = count - first.count
        return prefix + first.joined(separator: ", ") + (remainder > 0 ? ". \(remainder) more are shown on screen." : ".")
    }

    func start() async {
        if listening { stop(); return }
        guard !starting else { return }
        starting = true
        defer { starting = false }
        stopSpeaking()
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
