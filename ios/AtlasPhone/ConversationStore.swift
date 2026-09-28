import Combine
import Foundation

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

    private var api: AtlasAPI { AtlasAPI(server: server) }
    var isClosed: Bool { ["saved", "cancelled", "unsupported"].contains(conversation?.state ?? "") }

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
        do {
            try await api.health()
            connected = true
            if let id = UserDefaults.standard.string(forKey: "atlas.session"), !id.isEmpty {
                do { conversation = try await api.resume(id) }
                catch { message = "Could not resume the last conversation: " + error.localizedDescription }
            }
            await refreshSnapshot()
        } catch {
            connected = false
            message = error.localizedDescription
        }
    }

    func refreshSnapshot() async {
        guard !server.isEmpty else { return }
        do { snapshot = try await api.snapshot() }
        catch { snapshot = nil }
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

    func newConversation() {
        guard isClosed || conversation == nil else { return }
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
            if next.state == "saved" { await refreshSnapshot() }
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
