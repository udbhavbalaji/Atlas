import SwiftUI

private enum Palette {
    static let background = Color(red: 0.055, green: 0.098, blue: 0.078)
    static let surface = Color(red: 0.105, green: 0.165, blue: 0.125)
    static let elevated = Color(red: 0.16, green: 0.25, blue: 0.19)
    static let mint = Color(red: 0.74, green: 0.91, blue: 0.81)
    static let muted = Color(red: 0.66, green: 0.75, blue: 0.69)
}

struct ContentView: View {
    @EnvironmentObject private var store: ConversationStore
    @Environment(\.scenePhase) private var scenePhase
    @StateObject private var speech = SpeechController()
    @State private var showingConnection = false
    @State private var showingSaved = false
    @AppStorage("atlas.speakReplies") private var speakReplies = true
    @FocusState private var editorFocused: Bool

    var body: some View {
        NavigationStack {
            ScrollViewReader { reader in
                ScrollView {
                    VStack(alignment: .leading, spacing: 22) {
                        if let conversation = store.conversation {
                            conversationView(conversation)
                        } else {
                            startView
                        }
                        if !store.message.isEmpty {
                            Text(store.message)
                                .font(.footnote)
                                .foregroundStyle(Palette.muted)
                                .accessibilityIdentifier("atlas-status")
                        }
                        Color.clear.frame(height: 1).id("bottom")
                    }
                    .padding(20)
                }
                .scrollDismissesKeyboard(.interactively)
                .onChange(of: store.conversation?.version) { _, _ in
                    if speakReplies, let prompt = store.conversation?.prompt { speech.speak(prompt) }
                    withAnimation { reader.scrollTo("bottom", anchor: .bottom) }
                }
            }
            .background(Palette.background.ignoresSafeArea())
            .navigationTitle("Atlas")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .topBarLeading) {
                    Label(store.connected ? "Connected" : "Unavailable", systemImage: store.connected ? "circle.fill" : "circle.dotted")
                        .labelStyle(.titleAndIcon)
                        .font(.caption2)
                        .foregroundStyle(store.connected ? Palette.mint : .orange)
                }
                ToolbarItemGroup(placement: .topBarTrailing) {
                    Button { showingSaved = true } label: { Image(systemName: "tray.full") }
                        .accessibilityLabel("See saved items")
                    Button { showingConnection = true } label: { Image(systemName: "gearshape") }
                        .accessibilityLabel("Connection settings")
                }
            }
            .sheet(isPresented: $showingConnection) { ConnectionView() }
            .sheet(isPresented: $showingSaved) { SavedItemsView() }
            .task {
                if store.server.isEmpty { showingConnection = true }
                else { await store.reconnect() }
            }
            .onChange(of: scenePhase) { _, phase in
                if phase == .active { Task { await store.reconnect() } }
                if phase == .background { speech.stop() }
            }
            .onChange(of: speech.transcript) { _, text in
                guard !text.isEmpty else { return }
                if store.conversation == nil { store.setDraft(text) }
                else { store.setAnswer(text) }
            }
        }
        .tint(Palette.mint)
    }

    private var startView: some View {
        VStack(alignment: .leading, spacing: 20) {
            Text("YOUR SPACE TO THINK")
                .font(.caption2.weight(.bold)).tracking(2).foregroundStyle(Palette.mint)
            Text("What’s on your mind?")
                .font(.system(size: 40, weight: .bold, design: .rounded))
                .tracking(-1.7)
                .fixedSize(horizontal: false, vertical: true)
            Text("Tell Atlas what you want to do, remember, or be reminded about. You’ll review it before anything is saved.")
                .foregroundStyle(Palette.muted)
                .fixedSize(horizontal: false, vertical: true)
            editor(text: Binding(get: { store.draft }, set: { store.setDraft($0) }), hint: "I need to call Maya tomorrow evening…")
            HStack {
                microphoneButton
                Spacer()
                Button("Continue") { Task { await store.start() } }
                    .buttonStyle(PrimaryButtonStyle())
                    .disabled(store.busy || store.draft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
            }
            Text("Your draft stays on this phone until you send it.")
                .font(.footnote).foregroundStyle(Palette.muted)
            SavedSummaryView()
        }
        .padding(.top, 28)
    }

    private func conversationView(_ conversation: Conversation) -> some View {
        VStack(alignment: .leading, spacing: 18) {
            HStack {
                Text("CONVERSATION")
                    .font(.caption2.weight(.bold)).tracking(2).foregroundStyle(Palette.mint)
                Spacer()
                if store.isClosed {
                    Button("New") { store.newConversation() }
                        .font(.subheadline.weight(.semibold))
                }
            }
            ForEach(Array(conversation.messages.enumerated()), id: \.offset) { _, turn in
                VStack(alignment: .leading, spacing: 5) {
                    Text(turn.role == "user" ? "YOU" : "ATLAS")
                        .font(.caption2.weight(.bold)).tracking(1.5).foregroundStyle(Palette.muted)
                    Text(turn.text).textSelection(.enabled)
                }
                .padding(15)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(turn.role == "user" ? Palette.elevated : Palette.surface)
                .clipShape(RoundedRectangle(cornerRadius: 18))
            }
            VStack(alignment: .leading, spacing: 12) {
                Text("ATLAS ASKS")
                    .font(.caption2.weight(.bold)).tracking(1.8).foregroundStyle(Palette.mint)
                Text(conversation.prompt)
                    .font(.title3.weight(.medium))
                    .fixedSize(horizontal: false, vertical: true)
                if conversation.state == "awaiting_answer" {
                    let choices = conversation.question?.choices ?? []
                    if !choices.isEmpty {
                        Text("You can say: " + choices.map(\.label).joined(separator: " · "))
                            .font(.footnote).foregroundStyle(Palette.muted)
                    }
                }
            }
            .padding(20)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Palette.elevated)
            .clipShape(RoundedRectangle(cornerRadius: 20))

            if let proposal = conversation.proposal { proposalView(proposal, warnings: conversation.warnings) }
            else if !conversation.warnings.isEmpty {
                ForEach(conversation.warnings, id: \.self) { Text($0).font(.footnote).foregroundStyle(.orange) }
            }
            if conversation.state == "saved" {
                Label("Saved to Atlas", systemImage: "checkmark.circle.fill")
                    .foregroundStyle(Palette.mint).font(.headline)
            }
            if !["saved", "cancelled", "unsupported"].contains(conversation.state) {
                editor(text: Binding(get: { store.answer }, set: { store.setAnswer($0) }), hint: conversation.state == "confirming" ? "Say yes to retry saving" : conversation.state == "awaiting_confirmation" ? "Say yes to save, or tell Atlas what to change" : "Answer Atlas")
                HStack {
                    microphoneButton
                    Spacer()
                    Button("Send") { Task { await store.sendAnswer() } }
                        .buttonStyle(PrimaryButtonStyle())
                        .disabled(store.busy || store.answer.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                }
            }
            Text(speech.message)
                .font(.footnote).foregroundStyle(Palette.muted)
        }
    }

    private func proposalView(_ proposal: CaptureProposal, warnings: [String]) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("BEFORE SAVING").font(.caption2.weight(.bold)).tracking(1.8).foregroundStyle(Palette.mint)
            Text("Review this").font(.title2.weight(.bold))
            let input = proposal.input
            reviewRow("Type", input.kind)
            reviewRow("Title", input.title)
            reviewRow("Details", input.details)
            reviewRow("Deadline", displayDate(input.due_at))
            reviewRow("Reminder", displayDate(input.reminder_at))
            reviewRow("Repeat", input.`repeat`)
            reviewRow("Note", input.note_body)
            ForEach(Array((warnings + proposal.warnings).enumerated()), id: \.offset) { _, warning in
                Text(warning).font(.footnote).foregroundStyle(.orange)
            }
        }
        .padding(20)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Palette.surface)
        .clipShape(RoundedRectangle(cornerRadius: 20))
    }

    @ViewBuilder private func reviewRow(_ label: String, _ value: String) -> some View {
        if !value.isEmpty {
            VStack(alignment: .leading, spacing: 3) {
                Text(label.uppercased()).font(.caption2.weight(.bold)).foregroundStyle(Palette.muted)
                Text(value).textSelection(.enabled)
            }
            Divider().overlay(Palette.muted.opacity(0.2))
        }
    }

    private func editor(text: Binding<String>, hint: String) -> some View {
        TextEditor(text: text)
            .scrollContentBackground(.hidden)
            .frame(minHeight: 110)
            .padding(12)
            .background(Palette.surface)
            .clipShape(RoundedRectangle(cornerRadius: 18))
            .overlay(alignment: .topLeading) {
                if text.wrappedValue.isEmpty {
                    Text(hint).foregroundStyle(Palette.muted).padding(.top, 20).padding(.leading, 17)
                        .allowsHitTesting(false)
                }
            }
            .focused($editorFocused)
            .accessibilityLabel(hint)
    }

    private var microphoneButton: some View {
        Button {
            Task { await speech.start() }
        } label: {
            Label(speech.listening ? "Done" : "Microphone", systemImage: speech.listening ? "stop.circle.fill" : "mic.fill")
        }
        .buttonStyle(.bordered)
        .disabled(store.busy)
    }
}

private struct PrimaryButtonStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.headline)
            .foregroundStyle(Palette.background)
            .padding(.horizontal, 22).padding(.vertical, 13)
            .frame(minHeight: 48)
            .background(Palette.mint.opacity(configuration.isPressed ? 0.72 : 1))
            .clipShape(RoundedRectangle(cornerRadius: 15))
    }
}

private func displayDate(_ value: String) -> String {
    guard !value.isEmpty else { return "" }
    let parser = ISO8601DateFormatter()
    parser.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
    let date = parser.date(from: value) ?? ISO8601DateFormatter().date(from: value)
    return date?.formatted(date: .abbreviated, time: .shortened) ?? value
}
