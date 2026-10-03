import Foundation

enum AtlasAPIError: LocalizedError {
    case missingServer
    case invalidServer
    case unexpectedResponse
    case server(String, Int)

    var errorDescription: String? {
        switch self {
        case .missingServer: return "Set your Atlas server address in Connection first."
        case .invalidServer: return "Use an HTTPS Atlas address. HTTP is allowed only for localhost in Simulator."
        case .unexpectedResponse: return "Atlas sent a response this app could not read. Resume the conversation before retrying."
        case let .server(message, _): return message
        }
    }
}

struct AtlasAPI {
    let server: String
    private let decoder = JSONDecoder()

    static func normalizedServer(_ value: String) -> String? {
        let trimmed = value.trimmingCharacters(in: .whitespacesAndNewlines)
        guard let components = URLComponents(string: trimmed),
              let scheme = components.scheme?.lowercased(),
              let host = components.host?.lowercased(), !host.isEmpty,
              components.user == nil, components.password == nil,
              components.query == nil, components.fragment == nil,
              components.path.isEmpty || components.path == "/" else { return nil }
        guard scheme == "https" || (scheme == "http" && ["localhost", "127.0.0.1", "::1"].contains(host)) else { return nil }
        var clean = components
        clean.path = ""
        return clean.string?.trimmingCharacters(in: CharacterSet(charactersIn: "/"))
    }

    private func send<T: Decodable>(_ path: String, method: String = "GET", body: [String: Any]? = nil) async throws -> T {
        guard !server.isEmpty else { throw AtlasAPIError.missingServer }
        guard let base = Self.normalizedServer(server), let url = URL(string: base + path) else { throw AtlasAPIError.invalidServer }
        var request = URLRequest(url: url)
        request.httpMethod = method
        request.cachePolicy = .reloadIgnoringLocalCacheData
        request.timeoutInterval = 180
        if let body {
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
            request.httpBody = try JSONSerialization.data(withJSONObject: body)
        }
        let (data, response) = try await URLSession.shared.data(for: request)
        guard let response = response as? HTTPURLResponse else { throw AtlasAPIError.unexpectedResponse }
        guard (200..<300).contains(response.statusCode) else {
            let detail = try? decoder.decode(AtlasErrorEnvelope.self, from: data)
            throw AtlasAPIError.server(detail?.error.message ?? "Atlas request failed (\(response.statusCode)).", response.statusCode)
        }
        do { return try decoder.decode(T.self, from: data) }
        catch { throw AtlasAPIError.unexpectedResponse }
    }

    func health() async throws {
        struct Health: Decodable { let status: String }
        let result: Health = try await send("/healthz")
        guard result.status == "ok" else { throw AtlasAPIError.unexpectedResponse }
    }

    func start(_ text: String) async throws -> Conversation {
        try await send("/api/v1/conversations/routing", method: "POST", body: [
            "provider": "jev", "version": "1", "request_id": UUID().uuidString,
            "text": text, "timezone": TimeZone.current.identifier
        ])
    }

    func resume(_ id: String) async throws -> Conversation {
        let valid = id.unicodeScalars.allSatisfy { CharacterSet(charactersIn: "0123456789abcdefABCDEF").contains($0) }
        guard id.count == 32, valid else { throw AtlasAPIError.server("Enter a valid Atlas session ID.", 400) }
        return try await send("/api/v1/conversations/routing/" + id)
    }

    func reply(_ conversation: Conversation, text: String = "", field: String = "", value: String = "") async throws -> Conversation {
        try await send("/api/v1/conversations/routing/" + conversation.id + "/reply", method: "POST", body: [
            "version": conversation.version, "text": text, "field": field, "value": value
        ])
    }

    func snapshot() async throws -> SavedSnapshot {
        async let tasks: [AtlasTask] = send("/api/v1/tasks")
        async let reminders: [AtlasReminder] = send("/api/v1/reminders")
        async let notes: [AtlasNote] = send("/api/v1/notes")
        let (taskItems, reminderItems, noteItems) = try await (tasks, reminders, notes)
        return SavedSnapshot(tasks: taskItems, reminders: reminderItems, notes: noteItems)
    }
}
