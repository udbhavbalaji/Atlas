import AppKit
import Foundation
import WebKit

private enum StartupError: LocalizedError {
    case missingServer
    case invalidAnnouncement
    case unhealthyServer

    var errorDescription: String? {
        switch self {
        case .missingServer: return "The Atlas server is missing from this app bundle."
        case .invalidAnnouncement: return "The Atlas server did not report a local address."
        case .unhealthyServer: return "The Atlas server did not become ready."
        }
    }
}

private func readAnnouncement(_ handle: FileHandle) throws -> String {
    var bytes = Data()
    while bytes.count < 4096 {
        let next = handle.readData(ofLength: 1)
        if next.isEmpty { break }
        if next.first == 10 { break }
        bytes.append(next)
    }
    guard let line = String(data: bytes, encoding: .utf8),
          line.hasPrefix("ATLAS_URL=http://127.0.0.1:") else {
        throw StartupError.invalidAnnouncement
    }
    return String(line.dropFirst("ATLAS_URL=".count))
}

final class AtlasDelegate: NSObject, NSApplicationDelegate, WKNavigationDelegate, WKUIDelegate {
    private var window: NSWindow!
    private var webView: WKWebView!
    private var server: Process?
    private var startupFinished = false

    func applicationDidFinishLaunching(_ notification: Notification) {
        NSApp.setActivationPolicy(.regular)
        let rect = NSRect(x: 0, y: 0, width: 1100, height: 820)
        window = NSWindow(contentRect: rect,
                          styleMask: [.titled, .closable, .miniaturizable, .resizable],
                          backing: .buffered, defer: false)
        window.title = "Atlas"
        window.center()
        webView = WKWebView(frame: rect)
        webView.navigationDelegate = self
        webView.uiDelegate = self
        window.contentView = webView
        webView.loadHTMLString("<html><body style='background:#101114;color:#e7e9ed;font:16px system-ui;padding:40px'>Starting Atlas…</body></html>", baseURL: nil)
        window.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
        startServer()
    }

    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { true }

    func applicationWillTerminate(_ notification: Notification) {
        if let server = server, server.isRunning {
            server.terminate()
            server.waitUntilExit()
        }
    }

    private func startServer() {
        guard let serverURL = Bundle.main.resourceURL?.appendingPathComponent("atlas-server"),
              FileManager.default.isExecutableFile(atPath: serverURL.path) else {
            fail(StartupError.missingServer.localizedDescription)
            return
        }
        do {
            let support = try FileManager.default.url(for: .applicationSupportDirectory,
                                                      in: .userDomainMask, appropriateFor: nil, create: true)
            let dataDir = support.appendingPathComponent("Atlas", isDirectory: true)
            try FileManager.default.createDirectory(at: dataDir, withIntermediateDirectories: true,
                                                    attributes: [.posixPermissions: 0o700])
            let process = Process()
            let pipe = Pipe()
            process.executableURL = serverURL
            process.arguments = ["-addr", "127.0.0.1:0",
                                 "-db", dataDir.appendingPathComponent("atlas.db").path,
                                 "-openrouter-key-file", dataDir.appendingPathComponent("openrouter.key").path,
                                 "-groq-key-file", dataDir.appendingPathComponent("groq.key").path,
                                 "-announce-url"]
            process.standardOutput = pipe
            process.standardError = FileHandle.standardError
            try process.run()
            server = process
            DispatchQueue.global(qos: .userInitiated).async { [weak self] in
                do {
                    let url = try readAnnouncement(pipe.fileHandleForReading)
                    guard let health = URL(string: url + "/healthz"),
                          let page = URL(string: url + "/desktop") else {
                        throw StartupError.invalidAnnouncement
                    }
                    var healthy = false
                    for _ in 0..<50 {
                        if (try? Data(contentsOf: health)) != nil {
                            healthy = true
                            break
                        }
                        Thread.sleep(forTimeInterval: 0.1)
                    }
                    if !healthy { throw StartupError.unhealthyServer }
                    DispatchQueue.main.async {
                        guard let self = self, !self.startupFinished else { return }
                        self.startupFinished = true
                        self.webView.load(URLRequest(url: page))
                    }
                } catch {
                    DispatchQueue.main.async { self?.fail(error.localizedDescription) }
                }
            }
            DispatchQueue.main.asyncAfter(deadline: .now() + 15) { [weak self] in
                guard let self = self, !self.startupFinished else { return }
                self.fail("Atlas server startup timed out.")
            }
        } catch {
            fail(error.localizedDescription)
        }
    }

    private func fail(_ message: String) {
        guard !startupFinished else { return }
        startupFinished = true
        let alert = NSAlert()
        alert.messageText = "Atlas could not start"
        alert.informativeText = message
        alert.alertStyle = .critical
        alert.runModal()
        NSApp.terminate(nil)
    }

    func webView(_ webView: WKWebView, decidePolicyFor action: WKNavigationAction,
                 decisionHandler: @escaping (WKNavigationActionPolicy) -> Void) {
        guard let url = action.request.url else { decisionHandler(.cancel); return }
        if ["http", "https"].contains(url.scheme ?? "") && url.host != "127.0.0.1" {
            NSWorkspace.shared.open(url)
            decisionHandler(.cancel)
        } else {
            decisionHandler(.allow)
        }
    }

    func webView(_ webView: WKWebView, createWebViewWith configuration: WKWebViewConfiguration,
                 for action: WKNavigationAction, windowFeatures: WKWindowFeatures) -> WKWebView? {
        if action.targetFrame == nil { webView.load(action.request) }
        return nil
    }
}

let application = NSApplication.shared
let delegate = AtlasDelegate()
application.delegate = delegate
application.run()
