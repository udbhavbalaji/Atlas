import SwiftUI

private struct SavedGroup: View {
    let title: String
    let rows: [(String, String)]

    var body: some View {
        Section(title) {
            if rows.isEmpty {
                Text("None yet.").foregroundStyle(.secondary)
            } else {
                ForEach(Array(rows.enumerated()), id: \.offset) { _, row in
                    VStack(alignment: .leading, spacing: 4) {
                        Text(row.0)
                        if !row.1.isEmpty { Text(row.1).font(.caption).foregroundStyle(.secondary) }
                    }
                }
            }
        }
    }
}

struct SavedItemsView: View {
    @EnvironmentObject private var store: ConversationStore
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            List {
                if let snapshot = store.snapshot {
                    SavedGroup(title: "Open tasks", rows: snapshot.tasks.filter { $0.status == "open" }.prefix(20).map { ($0.title, $0.due_at.isEmpty ? "" : "Due " + $0.due_at) })
                    SavedGroup(title: "Reminders", rows: snapshot.reminders.filter { $0.status == "scheduled" || $0.status == "due" }.prefix(20).map { ($0.title, $0.status == "due" ? "Due" : $0.scheduled_at) })
                    SavedGroup(title: "Recent notes", rows: snapshot.notes.prefix(20).map { ($0.body, $0.created_at) })
                } else {
                    Text("Saved items are unavailable until Atlas reconnects.")
                        .foregroundStyle(.secondary)
                }
            }
            .navigationTitle("Saved in Atlas")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .topBarTrailing) { Button("Done") { dismiss() } } }
            .refreshable { await store.refreshSnapshot() }
            .task { await store.refreshSnapshot() }
        }
    }
}

struct SavedSummaryView: View {
    @EnvironmentObject private var store: ConversationStore

    var body: some View {
        DisclosureGroup("See what’s saved") {
            if let snapshot = store.snapshot {
                VStack(alignment: .leading, spacing: 10) {
                    Text("\(snapshot.tasks.filter { $0.status == "open" }.count) open tasks · \(snapshot.reminders.filter { $0.status == "due" || $0.status == "scheduled" }.count) reminders · \(snapshot.notes.count) notes")
                        .font(.footnote).foregroundStyle(.secondary)
                    ForEach(snapshot.tasks.filter { $0.status == "open" }.prefix(3)) { task in
                        Label(task.title, systemImage: "checklist")
                    }
                    ForEach(snapshot.reminders.filter { $0.status == "due" || $0.status == "scheduled" }.prefix(3)) { reminder in
                        Label(reminder.title, systemImage: "bell")
                    }
                    ForEach(snapshot.notes.prefix(2)) { note in
                        Label(note.body, systemImage: "note.text")
                            .lineLimit(2)
                    }
                }
                .padding(.vertical, 10)
            } else {
                Text("Connect to Atlas to see saved items.")
                    .font(.footnote).foregroundStyle(.secondary)
            }
        }
        .font(.subheadline)
        .padding(.top, 32)
    }
}
