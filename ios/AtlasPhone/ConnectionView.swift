import SwiftUI

struct ConnectionView: View {
    @EnvironmentObject private var store: ConversationStore
    @Environment(\.dismiss) private var dismiss
    @State private var address = ""
    @State private var sessionID = ""
    @AppStorage("atlas.speakReplies") private var speakReplies = true

    var body: some View {
        NavigationStack {
            Form {
                Section("Atlas server") {
                    TextField("https://atlas.example.com", text: $address)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .keyboardType(.URL)
                        .textContentType(.URL)
                        .accessibilityIdentifier("atlas-server")
                    Button("Save and connect") {
                        Task {
                            await store.setServer(address)
                            if store.connected { dismiss() }
                        }
                    }
                } footer: {
                    Text("Use a private HTTPS address reachable from your iPhone, including when you’re away from home. Atlas currently has no login, so keep the server behind a private network.")
                }
                Section("Conversation recovery") {
                    TextField("Session ID", text: $sessionID)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                    Button("Resume session") {
                        Task {
                            await store.resume(sessionID)
                            if store.conversation?.id == sessionID.trimmingCharacters(in: .whitespacesAndNewlines) { dismiss() }
                        }
                    }
                    .disabled(sessionID.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                } footer: {
                    Text("Your latest session normally resumes automatically after reconnecting.")
                }
                Section("Voice") {
                    Toggle("Read Atlas replies aloud", isOn: $speakReplies)
                }
                if !store.message.isEmpty {
                    Section { Text(store.message).foregroundStyle(.orange) }
                }
            }
            .navigationTitle("Connection")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .topBarTrailing) { Button("Done") { dismiss() } } }
            .onAppear { address = store.server }
        }
    }
}
