import SwiftUI

@main
struct AtlasPhoneApp: App {
    @StateObject private var conversation = ConversationStore()

    var body: some Scene {
        WindowGroup {
            ContentView()
                .environmentObject(conversation)
                .preferredColorScheme(.dark)
        }
    }
}
