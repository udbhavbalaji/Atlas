import Combine
import Foundation

private struct PendingConversationReport: Codable {
    let sessionID: String
    let version: Int
    let category: String
    let expected: String
    let messageIndex: Int
    let requestID: String

    func matches(_ conversation: Conversation, category: String, expected: String, messageIndex: Int) -> Bool {
        sessionID == conversation.id && version == conversation.version && self.category == category && self.expected == expected && self.messageIndex == messageIndex
    }
}

@MainActor
final class ConversationStore: ObservableObject {
    @Published var server = UserDefaults.standard.string(forKey: "atlas.server") ?? ""
    @Published var draft = UserDefaults.standard.string(forKey: "atlas.draft") ?? ""
    @Published var answer = UserDefaults.standard.string(forKey: "atlas.answer") ?? ""
    @Published private(set) var conversation: Conversation?
    @Published private(set) var snapshot: SavedSnapshot?
    @Published private(set) var busy = false
    @Published private(set) var connected = false
    @Published var message = ""
    private var lastRecordsRevision: Int64?
    private var snapshotRefreshInFlight = false

    private var api: AtlasAPI { AtlasAPI(server: server) }
    var isClosed: Bool { conversation?.state == "cancelled" }

    func setServer(_ value: String) async {
        guard let normalized = AtlasAPI.normalizedServer(value) else {
            message = AtlasAPIError.invalidServer.localizedDescription
            return
        }
        do { try await AtlasAPI(server: normalized).health() }
        catch { message = error.localizedDescription; return }
        if normalized == server { await reconnect(); return }
        server = normalized
        UserDefaults.standard.set(normalized, forKey: "atlas.server")
        conversation = nil
        UserDefaults.standard.removeObject(forKey: "atlas.session")
        snapshot = nil
        lastRecordsRevision = nil
        await reconnect()
    }

    func setDraft(_ value: String) {
        draft = value
        UserDefaults.standard.set(value, forKey: "atlas.draft")
    }

    func setAnswer(_ value: String) {
        answer = value
        UserDefaults.standard.set(value, forKey: "atlas.answer")
    }

    func reconnect() async {
        guard !server.isEmpty else { return }
        let target = server
        let client = AtlasAPI(server: target)
        do {
            try await client.health()
            guard server == target else { return }
            connected = true
            if let id = UserDefaults.standard.string(forKey: "atlas.session"), !id.isEmpty {
                do { conversation = try await client.resume(id) }
                catch { message = "Could not resume the last conversation: " + error.localizedDescription }
            }
            guard server == target else { return }
            let baseline = try? await client.recordsRevision()
            if await refreshSnapshot() {
                if server == target { lastRecordsRevision = baseline }
            }
        } catch {
            guard server == target else { return }
            connected = false
            message = error.localizedDescription
        }
    }

    @discardableResult func refreshSnapshot() async -> Bool {
        guard !server.isEmpty, !snapshotRefreshInFlight else { return false }
        let target = server
        snapshotRefreshInFlight = true
        defer { snapshotRefreshInFlight = false }
        do {
            let next = try await AtlasAPI(server: target).snapshot()
            guard server == target else { return false }
            snapshot = next
            connected = true
            return true
        } catch {
            guard server == target else { return false }
            if error is URLError { connected = false }
            return false
        }
    }

    func refreshIfChanged() async {
        guard !server.isEmpty else { return }
        let target = server
        do {
            let revision = try await AtlasAPI(server: target).recordsRevision()
            guard server == target else { return }
            connected = true
            if revision != lastRecordsRevision {
                if await refreshSnapshot() { lastRecordsRevision = revision }
            }
        } catch {
            guard server == target else { return }
            if error is URLError { connected = false }
        }
    }

    func start() async {
        let text = draft.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { return }
        await perform {
            let next = try await api.start(text)
            setDraft("")
            return next
        }
    }

    func sendAnswer() async {
        let text = answer.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty, let current = conversation else { return }
        await perform {
            let next = try await api.reply(current, text: text)
            setAnswer("")
            return next
        }
    }

    func reply(_ text: String, field: String = "", value: String = "") async {
        guard let current = conversation else { return }
        await perform { try await api.reply(current, text: text, field: field, value: value) }
    }

    func reportDraft() -> (category: String, expected: String, messageIndex: Int)? {
        guard let current = conversation, let pending = pendingReport(),
              pending.sessionID == current.id, pending.version == current.version else { return nil }
        return (pending.category, pending.expected, pending.messageIndex)
    }

    private func pendingReport() -> PendingConversationReport? {
        guard let data = UserDefaults.standard.data(forKey: "atlas.report.pending") else { return nil }
        return try? JSONDecoder().decode(PendingConversationReport.self, from: data)
    }

    func report(category: String, expected: String, messageIndex: Int) async -> Bool {
        guard !busy, let current = conversation else { return false }
        let previous = pendingReport()
        let requestID: String
        if let previous, previous.matches(current, category: category, expected: expected, messageIndex: messageIndex) {
            requestID = previous.requestID
        } else {
            requestID = UUID().uuidString
        }
        let pending = PendingConversationReport(sessionID: current.id, version: current.version,
            category: category, expected: expected, messageIndex: messageIndex,
            requestID: requestID)
        if let data = try? JSONEncoder().encode(pending) {
            UserDefaults.standard.set(data, forKey: "atlas.report.pending")
        }
        busy = true
        defer { busy = false }
        do {
            let receipt = try await api.report(current, requestID: pending.requestID, category: category, expected: expected, messageIndex: messageIndex)
            UserDefaults.standard.removeObject(forKey: "atlas.report.pending")
            message = "Report sent to Atlas · " + String(receipt.id.prefix(8))
            return true
        } catch {
            message = error.localizedDescription
            if let atlasError = error as? AtlasAPIError,
               case .server(_, 409) = atlasError {
                UserDefaults.standard.removeObject(forKey: "atlas.report.pending")
                do {
                    conversation = try await api.resume(current.id)
                    message = "Conversation changed. Review it, then open Report again."
                } catch { message += " Reconnect and review the conversation before retrying." }
            }
            return false
        }
    }

    func newConversation() {
        conversation = nil
        setAnswer("")
        UserDefaults.standard.removeObject(forKey: "atlas.session")
        message = "Ready for a new thought."
    }

    func resume(_ id: String) async {
        let trimmed = id.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return }
        await perform { try await api.resume(trimmed) }
    }

    private func perform(_ action: () async throws -> Conversation) async {
        guard !busy else { return }
        busy = true
        defer { busy = false }
        do {
            let next = try await action()
            conversation = next
            UserDefaults.standard.set(next.id, forKey: "atlas.session")
            connected = true
            message = next.state == "saved" ? "Saved to Atlas." : ""
            if next.state == "saved" || (next.state == "answered" && next.mutation != nil) {
                await refreshSnapshot()
            }
        } catch {
            message = error.localizedDescription
            if error is URLError { connected = false }
            if let atlasError = error as? AtlasAPIError,
               case .server(_, 409) = atlasError,
               let id = conversation?.id {
                do { conversation = try await api.resume(id) }
                catch { message += " Resume this conversation before retrying." }
            }
        }
    }

}
