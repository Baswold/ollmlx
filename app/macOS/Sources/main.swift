import Cocoa
import Foundation

// MARK: - Server Manager
class OllmlxServerManager {
    static let shared = OllmlxServerManager()

    private var serverProcess: Process?
    private var isRunning = false
    private let port: Int = 11434

    var statusCallback: ((Bool) -> Void)?

    private init() {}

    func start() {
        guard !isRunning else { return }

        let process = Process()

        // Find ollmlx binary
        let paths = [
            Bundle.main.resourceURL?.appendingPathComponent("ollmlx").path,
            "/usr/local/bin/ollmlx",
            "/opt/homebrew/bin/ollmlx",
            FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent(".ollmlx/ollmlx").path,
            "./ollmlx"
        ].compactMap { $0 }

        var binaryPath: String?
        for path in paths {
            if FileManager.default.fileExists(atPath: path) {
                binaryPath = path
                break
            }
        }

        guard let path = binaryPath else {
            showNotification(title: "ollmlx Error", message: "Could not find ollmlx binary. Please install it first.")
            return
        }

        process.executableURL = URL(fileURLWithPath: path)
        process.arguments = ["serve"]
        process.environment = ProcessInfo.processInfo.environment

        // Capture output for debugging
        let pipe = Pipe()
        process.standardOutput = pipe
        process.standardError = pipe

        process.terminationHandler = { [weak self] _ in
            DispatchQueue.main.async {
                self?.isRunning = false
                self?.statusCallback?(false)
            }
        }

        do {
            try process.run()
            serverProcess = process
            isRunning = true
            statusCallback?(true)
            showNotification(title: "ollmlx Started", message: "Server running on port \(port)")
        } catch {
            showNotification(title: "ollmlx Error", message: "Failed to start server: \(error.localizedDescription)")
        }
    }

    func stop() {
        guard isRunning, let process = serverProcess else { return }

        process.terminate()
        serverProcess = nil
        isRunning = false
        statusCallback?(false)
        showNotification(title: "ollmlx Stopped", message: "Server has been stopped")
    }

    func toggle() {
        if isRunning {
            stop()
        } else {
            start()
        }
    }

    func checkHealth() -> Bool {
        let url = URL(string: "http://localhost:\(port)/api/version")!
        var request = URLRequest(url: url)
        request.timeoutInterval = 1.0

        let semaphore = DispatchSemaphore(value: 0)
        var success = false

        let task = URLSession.shared.dataTask(with: request) { _, response, _ in
            if let httpResponse = response as? HTTPURLResponse {
                success = httpResponse.statusCode == 200
            }
            semaphore.signal()
        }
        task.resume()
        _ = semaphore.wait(timeout: .now() + 2.0)

        return success
    }

    var serverIsRunning: Bool { isRunning }

    private func showNotification(title: String, message: String) {
        let notification = NSUserNotification()
        notification.title = title
        notification.informativeText = message
        notification.soundName = nil
        NSUserNotificationCenter.default.deliver(notification)
    }
}

// MARK: - App Delegate
class AppDelegate: NSObject, NSApplicationDelegate {
    private var statusItem: NSStatusItem!
    private var menu: NSMenu!
    private var statusMenuItem: NSMenuItem!
    private var toggleMenuItem: NSMenuItem!
    private var healthCheckTimer: Timer?

    func applicationDidFinishLaunching(_ notification: Notification) {
        setupStatusItem()
        setupMenu()
        startHealthCheck()

        // Auto-start server if not already running
        if !OllmlxServerManager.shared.checkHealth() {
            OllmlxServerManager.shared.start()
        } else {
            updateStatus(running: true)
        }

        OllmlxServerManager.shared.statusCallback = { [weak self] running in
            self?.updateStatus(running: running)
        }
    }

    func applicationWillTerminate(_ notification: Notification) {
        OllmlxServerManager.shared.stop()
    }

    private func setupStatusItem() {
        statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)

        if let button = statusItem.button {
            button.image = createStatusIcon(running: false)
            button.image?.isTemplate = true
            button.toolTip = "ollmlx - MLX-Powered LLM Server"
        }
    }

    private func createStatusIcon(running: Bool) -> NSImage {
        let config = NSImage.SymbolConfiguration(pointSize: 16, weight: .medium)
        let symbolName = running ? "brain.head.profile" : "brain"

        if let image = NSImage(systemSymbolName: symbolName, accessibilityDescription: "ollmlx") {
            return image.withSymbolConfiguration(config) ?? image
        }

        // Fallback: create a simple circle icon
        let size = NSSize(width: 18, height: 18)
        let image = NSImage(size: size)
        image.lockFocus()

        let color: NSColor = running ? .systemGreen : .systemGray
        color.setFill()

        let rect = NSRect(x: 3, y: 3, width: 12, height: 12)
        NSBezierPath(ovalIn: rect).fill()

        image.unlockFocus()
        return image
    }

    private func setupMenu() {
        menu = NSMenu()

        // Header
        let headerItem = NSMenuItem(title: "ollmlx", action: nil, keyEquivalent: "")
        headerItem.isEnabled = false
        menu.addItem(headerItem)

        menu.addItem(NSMenuItem.separator())

        // Status
        statusMenuItem = NSMenuItem(title: "Status: Checking...", action: nil, keyEquivalent: "")
        statusMenuItem.isEnabled = false
        menu.addItem(statusMenuItem)

        menu.addItem(NSMenuItem.separator())

        // Toggle Server
        toggleMenuItem = NSMenuItem(title: "Start Server", action: #selector(toggleServer), keyEquivalent: "s")
        toggleMenuItem.target = self
        menu.addItem(toggleMenuItem)

        // Open in Browser
        let browserItem = NSMenuItem(title: "Open API in Browser", action: #selector(openInBrowser), keyEquivalent: "o")
        browserItem.target = self
        menu.addItem(browserItem)

        menu.addItem(NSMenuItem.separator())

        // Models submenu
        let modelsItem = NSMenuItem(title: "Models", action: nil, keyEquivalent: "")
        let modelsSubmenu = NSMenu()

        let listModelsItem = NSMenuItem(title: "List Models", action: #selector(listModels), keyEquivalent: "l")
        listModelsItem.target = self
        modelsSubmenu.addItem(listModelsItem)

        let pullModelItem = NSMenuItem(title: "Pull Model...", action: #selector(pullModel), keyEquivalent: "p")
        pullModelItem.target = self
        modelsSubmenu.addItem(pullModelItem)

        modelsItem.submenu = modelsSubmenu
        menu.addItem(modelsItem)

        menu.addItem(NSMenuItem.separator())

        // Help
        let helpItem = NSMenuItem(title: "Documentation", action: #selector(openDocs), keyEquivalent: "")
        helpItem.target = self
        menu.addItem(helpItem)

        // About
        let aboutItem = NSMenuItem(title: "About ollmlx", action: #selector(showAbout), keyEquivalent: "")
        aboutItem.target = self
        menu.addItem(aboutItem)

        menu.addItem(NSMenuItem.separator())

        // Quit
        let quitItem = NSMenuItem(title: "Quit ollmlx", action: #selector(quitApp), keyEquivalent: "q")
        quitItem.target = self
        menu.addItem(quitItem)

        statusItem.menu = menu
    }

    private func startHealthCheck() {
        healthCheckTimer = Timer.scheduledTimer(withTimeInterval: 5.0, repeats: true) { [weak self] _ in
            let running = OllmlxServerManager.shared.checkHealth()
            self?.updateStatus(running: running)
        }
    }

    private func updateStatus(running: Bool) {
        DispatchQueue.main.async { [weak self] in
            guard let self = self else { return }

            if running {
                self.statusMenuItem.title = "Status: Running on port 11434"
                self.toggleMenuItem.title = "Stop Server"
                self.statusItem.button?.image = self.createStatusIcon(running: true)
                self.statusItem.button?.image?.isTemplate = true
            } else {
                self.statusMenuItem.title = "Status: Stopped"
                self.toggleMenuItem.title = "Start Server"
                self.statusItem.button?.image = self.createStatusIcon(running: false)
                self.statusItem.button?.image?.isTemplate = true
            }
        }
    }

    @objc private func toggleServer() {
        OllmlxServerManager.shared.toggle()
    }

    @objc private func openInBrowser() {
        if let url = URL(string: "http://localhost:11434/api/version") {
            NSWorkspace.shared.open(url)
        }
    }

    @objc private func listModels() {
        let task = Process()
        task.executableURL = URL(fileURLWithPath: "/usr/bin/osascript")
        task.arguments = ["-e", """
            tell application "Terminal"
                activate
                do script "ollmlx list"
            end tell
        """]
        try? task.run()
    }

    @objc private func pullModel() {
        let alert = NSAlert()
        alert.messageText = "Pull Model"
        alert.informativeText = "Enter the model name (e.g., mlx-community/Llama-3.2-1B-Instruct-4bit):"
        alert.alertStyle = .informational
        alert.addButton(withTitle: "Pull")
        alert.addButton(withTitle: "Cancel")

        let textField = NSTextField(frame: NSRect(x: 0, y: 0, width: 400, height: 24))
        textField.placeholderString = "mlx-community/gemma-2-2b-it-4bit"
        alert.accessoryView = textField

        if alert.runModal() == .alertFirstButtonReturn {
            let modelName = textField.stringValue
            if !modelName.isEmpty {
                let task = Process()
                task.executableURL = URL(fileURLWithPath: "/usr/bin/osascript")
                task.arguments = ["-e", """
                    tell application "Terminal"
                        activate
                        do script "ollmlx pull \(modelName)"
                    end tell
                """]
                try? task.run()
            }
        }
    }

    @objc private func openDocs() {
        if let url = URL(string: "https://github.com/ollama/ollama") {
            NSWorkspace.shared.open(url)
        }
    }

    @objc private func showAbout() {
        let alert = NSAlert()
        alert.messageText = "ollmlx"
        alert.informativeText = """
        Apple Silicon Optimized LLM Inference
        100% Ollama Compatible | MLX-Powered

        A high-performance LLM server for macOS,
        optimized for M1/M2/M3/M4 Macs.

        Version 1.0.0
        """
        alert.alertStyle = .informational
        alert.addButton(withTitle: "OK")
        alert.runModal()
    }

    @objc private func quitApp() {
        OllmlxServerManager.shared.stop()
        NSApplication.shared.terminate(nil)
    }
}

// MARK: - Main
let app = NSApplication.shared
let delegate = AppDelegate()
app.delegate = delegate
app.setActivationPolicy(.accessory)
app.run()
