// loafer's notifier: posts one notification, given as arguments, and
// when it's clicked runs `loafer open` on where it came from. macOS
// starts it again for a click after it's quit. Built by app.go.
import AppKit
import UserNotifications

@main
final class Notifier: NSObject, NSApplicationDelegate, UNUserNotificationCenterDelegate {
    static func main() {
        let app = NSApplication.shared
        let d = Notifier()
        app.delegate = d
        app.run()
    }

    let center = UNUserNotificationCenter.current()
    let bin = Bundle.main.object(forInfoDictionaryKey: "LoaferBinary") as? String ?? "loafer"
    let keys = ["team", "conv", "ts", "thread"]

    func applicationDidFinishLaunching(_ n: Notification) {
        center.delegate = self
        // post <title> <body> <team> <conv> <ts> <thread>
        let args = Array(CommandLine.arguments.dropFirst())
        guard args.count == 7, args[0] == "post" else {
            quit(after: 10) // started for a click: didReceive comes soon
            return
        }
        quit(after: 30) // the first asks you to allow notifications, which may sit unanswered
        center.requestAuthorization(options: [.alert, .sound]) { [self] ok, _ in
            guard ok else { return quit(after: 0) }
            let c = UNMutableNotificationContent()
            c.title = args[1]
            c.body = args[2]
            c.userInfo = Dictionary(uniqueKeysWithValues: zip(keys, args[3...]))
            c.threadIdentifier = args[4] // a conversation's stack together
            center.add(UNNotificationRequest(identifier: UUID().uuidString, content: c, trigger: nil)) { [self] _ in
                quit(after: 1)
            }
        }
    }

    func userNotificationCenter(_ c: UNUserNotificationCenter, willPresent n: UNNotification,
                                withCompletionHandler done: @escaping (UNNotificationPresentationOptions) -> Void) {
        done([.banner, .list])
    }

    func userNotificationCenter(_ c: UNUserNotificationCenter, didReceive r: UNNotificationResponse,
                                withCompletionHandler done: @escaping () -> Void) {
        defer { done() }
        let info = r.notification.request.content.userInfo
        guard r.actionIdentifier == UNNotificationDefaultActionIdentifier else { return quit(after: 0) }
        var args = ["open"] // a burst's has nowhere, and only brings loafer forward
        for k in keys {
            if let v = info[k] as? String, !v.isEmpty { args.append(v) }
        }
        let p = Process()
        p.executableURL = URL(fileURLWithPath: bin)
        p.arguments = args
        try? p.run()
        quit(after: 2)
    }

    func quit(after s: Double) {
        DispatchQueue.main.asyncAfter(deadline: .now() + s) { NSApp.terminate(nil) }
    }
}
