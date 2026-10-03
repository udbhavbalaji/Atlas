import Foundation

struct Conversation: Decodable {
    let id: String
    let version: Int
    let state: String
    let prompt: String
    let question: ConversationQuestion?
    let proposal: CaptureProposal?
    let mutation: ConversationMutation?
    let warnings: [String]
    let messages: [ConversationMessage]
}

struct ConversationMutation: Decodable {
    let action: String
}

struct ConversationMessage: Decodable, Identifiable {
    let role: String
    let text: String
    var id: String { role + ":" + text }
}

struct ConversationQuestion: Decodable {
    let field: String
    let choices: [ConversationChoice]?
}

struct ConversationChoice: Decodable, Identifiable {
    let value: String
    let label: String
    var id: String { value }
}

struct CaptureProposal: Decodable {
    let input: CaptureInput
    let warnings: [String]
}

struct CaptureInput: Decodable {
    let kind: String
    let title: String
    let details: String
    let due_at: String
    let reminder_at: String
    let `repeat`: String
    let note_body: String
    let timezone: String
}

struct AtlasTask: Decodable, Identifiable {
    let id: String
    let title: String
    let status: String
    let due_at: String
}

struct AtlasReminder: Decodable, Identifiable {
    let id: String
    let title: String
    let status: String
    let scheduled_at: String
}

struct AtlasNote: Decodable, Identifiable {
    let id: String
    let body: String
    let created_at: String
}

struct SavedSnapshot {
    let tasks: [AtlasTask]
    let reminders: [AtlasReminder]
    let notes: [AtlasNote]
}

struct AtlasErrorEnvelope: Decodable {
    let error: AtlasErrorDetail
}

struct AtlasErrorDetail: Decodable {
    let message: String
    let code: String?
}
