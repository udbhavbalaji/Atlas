import SwiftUI

private enum Palette {
    static let background = Color(red: 0.957, green: 0.965, blue: 0.949)
    static let surface = Color.white
    static let elevated = Color(red: 0.884, green: 0.941, blue: 0.895)
    static let accent = Color(red: 0.141, green: 0.357, blue: 0.286)
    static let ink = Color(red: 0.098, green: 0.231, blue: 0.188)
    static let muted = Color(red: 0.388, green: 0.486, blue: 0.424)
}

struct ContentView: View {
    @EnvironmentObject private var store: ConversationStore
    @Environment(\.scenePhase) private var scenePhase
    @StateObject private var speech = SpeechController()
    @State private var showingConnection = false
    @State private var showingSaved = false
    @State private var showingReport = false
    @State private var selectedTargets: Set<String> = []
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
                    selectedTargets.removeAll()
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
                        .foregroundStyle(store.connected ? Palette.accent : .orange)
                }
                ToolbarItemGroup(placement: .topBarTrailing) {
                    if store.conversation != nil {
                        Button { showingReport = true } label: { Image(systemName: "exclamationmark.bubble") }
                            .accessibilityLabel("Report conversation issue")
                    }
                    Button { showingSaved = true } label: { Image(systemName: "tray.full") }
                        .accessibilityLabel("See saved items")
                    Button { showingConnection = true } label: { Image(systemName: "gearshape") }
                        .accessibilityLabel("Connection settings")
                }
            }
            .sheet(isPresented: $showingConnection) { ConnectionView() }
            .sheet(isPresented: $showingSaved) { SavedItemsView() }
            .sheet(isPresented: $showingReport) { ReportView() }
            .task {
                if store.server.isEmpty { showingConnection = true }
                else { await store.reconnect() }
            }
            .task(id: scenePhase) {
                guard scenePhase == .active else { return }
                while !Task.isCancelled {
                    await store.refreshIfChanged()
                    try? await Task.sleep(nanoseconds: 1_000_000_000)
                }
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
        .tint(Palette.accent)
    }

    private var startView: some View {
        VStack(alignment: .leading, spacing: 20) {
            Text("YOUR PRIVATE WORKSPACE")
                .font(.caption2.weight(.bold)).tracking(2).foregroundStyle(Palette.accent)
            Text("A little more clarity, every day.")
                .font(.system(size: 40, weight: .bold, design: .rounded))
                .tracking(-1.7)
                .fixedSize(horizontal: false, vertical: true)
            Text("Ask about your tasks, reminders, and notes. Add something new, or make a change with a clear review.")
                .foregroundStyle(Palette.muted)
                .fixedSize(horizontal: false, vertical: true)
            editor(text: Binding(get: { store.draft }, set: { store.setDraft($0) }), hint: "I need to call Maya tomorrow evening…")
            HStack {
                microphoneButton
                Spacer()
                Button("Send message") { Task { await store.start() } }
                    .buttonStyle(PrimaryButtonStyle())
                    .disabled(store.busy || store.draft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
            }
            Text("Your draft stays on this phone until you send it to your Atlas service.")
                .font(.footnote).foregroundStyle(Palette.muted)
            SavedSummaryView()
        }
        .padding(.top, 24)
    }

    private func conversationView(_ conversation: Conversation) -> some View {
        VStack(alignment: .leading, spacing: 18) {
            HStack {
                Text("YOUR CONVERSATION")
                    .font(.caption2.weight(.bold)).tracking(2).foregroundStyle(Palette.accent)
                Spacer()
                Button("Report") { showingReport = true }
                    .font(.subheadline.weight(.semibold))
                    .accessibilityIdentifier("atlas-report-button")
                Button("New") { store.newConversation() }
                    .font(.subheadline.weight(.semibold))
                    .disabled(store.busy)
            }
            ForEach(Array(conversation.messages.enumerated()), id: \.offset) { _, turn in
                HStack {
                    if turn.role == "user" { Spacer(minLength: 36) }
                    VStack(alignment: .leading, spacing: 5) {
                        Text(turn.role == "user" ? "YOU" : "ATLAS")
                            .font(.caption2.weight(.bold)).tracking(1.5).foregroundStyle(Palette.muted)
                        Text(turn.text).foregroundStyle(Palette.ink).textSelection(.enabled)
                    }
                    .padding(15)
                    .background(turn.role == "user" ? Palette.elevated : Palette.surface)
                    .clipShape(RoundedRectangle(cornerRadius: 18))
                    .overlay(RoundedRectangle(cornerRadius: 18).stroke(Palette.accent.opacity(0.08)))
                    if turn.role != "user" { Spacer(minLength: 36) }
                }
            }
            if !["saved", "answered"].contains(conversation.state) {
                VStack(alignment: .leading, spacing: 12) {
                    Text("NEXT STEP")
                        .font(.caption2.weight(.bold)).tracking(1.8).foregroundStyle(Palette.accent)
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
            }

            if let workflow = conversation.workflow {
                VStack(alignment: .leading, spacing: 8) {
                    Text("ORDERED PLAN").font(.caption2.weight(.bold)).tracking(1.5).foregroundStyle(Palette.accent)
                    ForEach(Array(workflow.steps.enumerated()), id: \.offset) { index, step in
                        Text("\(index + 1). \(step) · \(index < workflow.index ? "complete" : index == workflow.index ? "current" : "pending")")
                            .font(.subheadline)
                    }
                }
                .padding(17).frame(maxWidth: .infinity, alignment: .leading)
                .background(Palette.surface).clipShape(RoundedRectangle(cornerRadius: 18))
            }
            if let batch = conversation.batch {
                batchView(batch, choosing: conversation.state == "awaiting_batch_targets")
            }
            if let batch = conversation.batch_edit {
                batchView(batch, choosing: conversation.state == "awaiting_batch_edit_targets")
            }

            if let proposal = conversation.proposal, conversation.state == "awaiting_confirmation" { proposalView(proposal, warnings: conversation.warnings) }
            else if !conversation.warnings.isEmpty {
                ForEach(conversation.warnings, id: \.self) { Text($0).font(.footnote).foregroundStyle(.orange) }
            }
            if conversation.state == "saved" {
                Label("Saved to Atlas", systemImage: "checkmark.circle.fill")
                    .foregroundStyle(Palette.accent).font(.headline)
            }
            reviewActions(conversation)
            if !store.isClosed {
                editor(text: Binding(get: { store.answer }, set: { store.setAnswer($0) }), hint: replyHint(conversation.state))
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

    private func batchView(_ batch: ConversationBatch, choosing: Bool) -> some View {
        let items = choosing ? (batch.candidates ?? []) : batch.targets
        return VStack(alignment: .leading, spacing: 10) {
            Text(choosing ? "CHOOSE EXACT RECORDS" : "REVIEW EXACT RECORDS")
                .font(.caption2.weight(.bold)).tracking(1.5).foregroundStyle(Palette.accent)
            ForEach(items) { item in
                if choosing {
                    Toggle(isOn: Binding(
                        get: { selectedTargets.contains(item.id) },
                        set: { selected in
                            if selected { selectedTargets.insert(item.id) }
                            else { selectedTargets.remove(item.id) }
                        }
                    )) {
                        Text(item.kind.capitalized + " · " + item.title).font(.subheadline)
                    }
                } else {
                    Label(item.kind.capitalized + " · " + item.title, systemImage: "checkmark.circle")
                        .font(.subheadline)
                }
            }
            ForEach(batch.effects ?? [], id: \.self) { effect in
                Text(effect).font(.footnote).foregroundStyle(Palette.muted)
            }
            if choosing {
                Button("Review selected records") {
                    Task { await store.reply("", field: "batch_targets", value: selectedTargets.sorted().joined(separator: ",")) }
                }
                .buttonStyle(PrimaryButtonStyle())
                .disabled(selectedTargets.isEmpty || store.busy)
            }
        }
        .padding(18).frame(maxWidth: .infinity, alignment: .leading)
        .background(Palette.surface).clipShape(RoundedRectangle(cornerRadius: 20))
    }

    @ViewBuilder private func reviewActions(_ conversation: Conversation) -> some View {
        let state = conversation.state
        if state == "awaiting_workflow_start" {
            Button("Start first step") { Task { await store.reply("start") } }
                .buttonStyle(PrimaryButtonStyle()).disabled(store.busy)
        } else if let workflow = conversation.workflow,
                  ["answered", "saved"].contains(state), workflow.index + 1 < workflow.steps.count {
            Button("Continue to next step") { Task { await store.reply("continue") } }
                .buttonStyle(PrimaryButtonStyle()).disabled(store.busy)
        } else if state == "unsupported", conversation.workflow != nil {
            Button("Skip this step") { Task { await store.reply("skip") } }
                .buttonStyle(.bordered).disabled(store.busy)
        }
        if ["awaiting_confirmation", "awaiting_delete_confirmation", "awaiting_batch_edit_confirmation", "awaiting_association_confirmation", "awaiting_occurrence_confirmation"].contains(state) {
            HStack {
                Button("Cancel") { Task { await store.reply("cancel") } }
                    .buttonStyle(.bordered)
                Spacer()
                Button(state == "awaiting_delete_confirmation" ? "Confirm deletion" : "Confirm change") {
                    Task { await store.reply("yes") }
                }
                .buttonStyle(PrimaryButtonStyle())
            }
            .disabled(store.busy)
        }
        if let question = conversation.question, let choices = question.choices,
           ["awaiting_answer", "awaiting_target", "awaiting_field"].contains(state) {
            ForEach(choices) { choice in
                Button(choice.label) { Task { await store.reply("", field: question.field, value: choice.value) } }
                    .buttonStyle(.bordered).disabled(store.busy)
            }
        }
    }

    private func replyHint(_ state: String) -> String {
        switch state {
        case "confirming", "deleting": return "Say yes to retry the action"
        case "awaiting_confirmation": return "Say yes to save, or tell Atlas what to change"
        case "awaiting_delete_confirmation": return "Say yes to delete, or cancel"
        case "saved", "answered": return "Ask Atlas something else…"
        default: return "Answer Atlas"
        }
    }

    private func proposalView(_ proposal: CaptureProposal, warnings: [String]) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("BEFORE SAVING").font(.caption2.weight(.bold)).tracking(1.8).foregroundStyle(Palette.accent)
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

private struct ReportView: View {
    @EnvironmentObject private var store: ConversationStore
    @Environment(\.dismiss) private var dismiss
    @State private var category = "wrong_answer"
    @State private var expected = ""
    @State private var messageIndex = -1
    @State private var errorMessage = ""

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Picker("What went wrong?", selection: $category) {
                        Text("Wrong answer").tag("wrong_answer")
                        Text("Missed part of my request").tag("missed_action")
                        Text("Unwanted change").tag("unwanted_change")
                        Text("Confusing question or review").tag("confusing_flow")
                        Text("Something else").tag("other")
                    }
                    if let messages = store.conversation?.messages {
                        Picker("Which part?", selection: $messageIndex) {
                            Text("Whole conversation").tag(-1)
                            ForEach(Array(messages.enumerated()), id: \.offset) { index, turn in
                                Text((turn.role == "user" ? "You: " : "Atlas: ") + String(turn.text.prefix(45)))
                                    .tag(index)
                            }
                        }
                    }
                } header: { Text("Conversation") }
                Section {
                    TextEditor(text: $expected)
                        .frame(minHeight: 125)
                        .accessibilityLabel("What should Atlas have done?")
                } header: { Text("What should Atlas have done?") }
                  footer: { Text("Your note, the conversation, and a route summary are sent to your connected Atlas service. Nothing is sent to an external issue tracker.") }
                if !errorMessage.isEmpty {
                    Section { Text(errorMessage).foregroundStyle(.red).font(.footnote) }
                }
            }
            .scrollContentBackground(.hidden)
            .background(Palette.background)
            .navigationTitle("Report an issue")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .topBarLeading) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .topBarTrailing) {
                    Button("Save report") {
                        Task {
                            let submittedVersion = store.conversation?.version
                            let saved = await store.report(category: category, expected: expected.trimmingCharacters(in: .whitespacesAndNewlines), messageIndex: messageIndex)
                            if saved { dismiss() }
                            else if store.conversation?.version != submittedVersion { dismiss() }
                            else { errorMessage = store.message }
                        }
                    }
                    .fontWeight(.semibold)
                    .disabled(store.busy || !(3...2000).contains(expected.trimmingCharacters(in: .whitespacesAndNewlines).count))
                }
            }
            .onAppear {
                if let draft = store.reportDraft() {
                    category = draft.category
                    expected = draft.expected
                    messageIndex = draft.messageIndex
                }
            }
        }
        .tint(Palette.accent)
    }
}

private struct PrimaryButtonStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.headline)
            .foregroundStyle(.white)
            .padding(.horizontal, 22).padding(.vertical, 13)
            .frame(minHeight: 48)
            .background(Palette.accent.opacity(configuration.isPressed ? 0.72 : 1))
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
