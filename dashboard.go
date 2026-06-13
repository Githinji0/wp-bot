package main

import (
	"encoding/json"
	"html/template"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
)

// DashboardMessage represents a captured message logged on the dashboard.
type DashboardMessage struct {
	Sender    string `json:"sender"`
	PushName  string `json:"push_name"`
	Content   string `json:"content"`
	Timestamp string `json:"timestamp"`
	IsGroup   bool   `json:"is_group"`
}

// Global state shared with main.go and handler.go
var (
	WhatsAppClient    *whatsmeow.Client
	CurrentQRCode     string
	CurrentQRCodeLock sync.Mutex

	RecentMessages     []DashboardMessage
	RecentMessagesLock sync.Mutex
)

// AddDashboardMessage appends a new message to the circular recent messages buffer.
func AddDashboardMessage(sender, pushName, content string, isGroup bool) {
	RecentMessagesLock.Lock()
	defer RecentMessagesLock.Unlock()

	msg := DashboardMessage{
		Sender:    sender,
		PushName:  pushName,
		Content:   content,
		Timestamp: time.Now().Format("15:04:05"),
		IsGroup:   isGroup,
	}

	RecentMessages = append(RecentMessages, msg)
	if len(RecentMessages) > 50 {
		RecentMessages = RecentMessages[1:]
	}
}

// StartDashboardServer spins up the Web Dashboard server.
func StartDashboardServer(preferredPort string) {
	http.HandleFunc("/", handleIndex)
	http.HandleFunc("/api/status", handleStatus)
	http.HandleFunc("/api/qr", handleQR)
	http.HandleFunc("/api/config", handleConfigUpdate)
	http.HandleFunc("/api/messages", handleMessagesList)

	go func() {
		ports := []string{preferredPort, "8081", "8082", "8085", "9000"}
		var listener net.Listener
		var err error
		var selectedPort string

		for _, p := range ports {
			listener, err = net.Listen("tcp", ":"+p)
			if err == nil {
				selectedPort = p
				break
			}
		}

		if err != nil {
			log.Printf("❌ Failed to start Dashboard: all fallback ports are occupied. error: %v", err)
			return
		}

		log.Printf("🌐 Bot Dashboard is running on http://localhost:%s", selectedPort)
		if err := http.Serve(listener, nil); err != nil {
			log.Printf("❌ Dashboard Server error: %v", err)
		}
	}()
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	tmpl, err := template.New("dashboard").Parse(indexHTML)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = tmpl.Execute(w, nil)
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	cfg := GetConfig()

	connected := false
	pushName := ""
	jid := ""

	if WhatsAppClient != nil {
		connected = WhatsAppClient.IsConnected() && WhatsAppClient.Store.ID != nil
		if connected {
			pushName = WhatsAppClient.Store.PushName
			jid = WhatsAppClient.Store.ID.String()
		}
	}

	resp := map[string]interface{}{
		"connected":  connected,
		"push_name":  pushName,
		"jid":        jid,
		"groq_model": cfg.GroqModel,
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func handleQR(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	connected := false
	if WhatsAppClient != nil {
		connected = WhatsAppClient.IsConnected() && WhatsAppClient.Store.ID != nil
	}

	CurrentQRCodeLock.Lock()
	qrCode := CurrentQRCode
	CurrentQRCodeLock.Unlock()

	resp := map[string]interface{}{
		"connected": connected,
		"qr":        qrCode,
	}
	_ = json.NewEncoder(w).Encode(resp)
}

type configUpdateRequest struct {
	AwayMode     bool     `json:"away_mode"`
	AwayMessage  string   `json:"away_message"`
	BlockedWords []string `json:"blocked_words"`
	GroqModel    string   `json:"groq_model"`
	AIAssist     bool     `json:"ai_assist"`
}

func handleConfigUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		// Just return current config values
		w.Header().Set("Content-Type", "application/json")
		cfg := GetConfig()
		resp := map[string]interface{}{
			"away_mode":     cfg.AwayMode,
			"away_message":  cfg.AwayMessage,
			"blocked_words": cfg.BlockedWords,
			"groq_model":    cfg.GroqModel,
			"ai_assist":     cfg.AIAssist,
		}
		_ = json.NewEncoder(w).Encode(resp)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}

	var req configUpdateRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	// Dynamic update in memory and persistence in .env
	err = UpdateConfig(req.AwayMode, req.AwayMessage, req.BlockedWords, req.GroqModel, req.AIAssist)
	if err != nil {
		log.Printf("❌ Failed to update configuration: %v", err)
		http.Error(w, "Internal server error saving settings", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	resp := map[string]interface{}{
		"success": true,
		"message": "Settings updated and saved to .env successfully",
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func handleMessagesList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	RecentMessagesLock.Lock()
	msgs := make([]DashboardMessage, len(RecentMessages))
	copy(msgs, RecentMessages)
	RecentMessagesLock.Unlock()

	_ = json.NewEncoder(w).Encode(msgs)
}

// Embedded Web Dashboard Interface HTML/CSS/JS
const indexHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>WhatsApp Bot Dashboard</title>
    <link href="https://fonts.googleapis.com/css2?family=Outfit:wght@300;400;600;800&display=swap" rel="stylesheet">
    <script src="https://cdnjs.cloudflare.com/ajax/libs/qrcodejs/1.0.0/qrcode.min.js"></script>
    <style>
        :root {
            --bg-color: #0b0f19;
            --card-bg: rgba(22, 28, 45, 0.45);
            --card-border: rgba(255, 255, 255, 0.08);
            --text-main: #f3f4f6;
            --text-muted: #9ca3af;
            --accent: #10b981;
            --accent-glow: rgba(16, 185, 129, 0.15);
            --accent-danger: #ef4444;
            --accent-warn: #f59e0b;
        }

        * {
            box-sizing: border-box;
            margin: 0;
            padding: 0;
        }

        body {
            font-family: 'Outfit', sans-serif;
            background-color: var(--bg-color);
            color: var(--text-main);
            min-height: 100vh;
            display: flex;
            flex-direction: column;
            align-items: center;
            padding: 2rem 1rem;
            background-image: radial-gradient(circle at 10% 20%, rgba(16, 185, 129, 0.05) 0%, transparent 40%),
                              radial-gradient(circle at 90% 80%, rgba(99, 102, 241, 0.05) 0%, transparent 40%);
        }

        .container {
            width: 100%;
            max-width: 800px;
            display: flex;
            flex-direction: column;
            gap: 1.5rem;
        }

        header {
            display: flex;
            justify-content: space-between;
            align-items: center;
            background: var(--card-bg);
            border: 1px solid var(--card-border);
            border-radius: 16px;
            padding: 1.5rem;
            backdrop-filter: blur(12px);
            box-shadow: 0 4px 30px rgba(0, 0, 0, 0.2);
        }

        h1 {
            font-size: 1.8rem;
            font-weight: 800;
            background: linear-gradient(135deg, #10b981 0%, #3b82f6 100%);
            -webkit-background-clip: text;
            -webkit-text-fill-color: transparent;
        }

        .status-badge {
            display: flex;
            align-items: center;
            gap: 0.5rem;
            font-size: 0.85rem;
            font-weight: 600;
            padding: 0.5rem 1rem;
            border-radius: 9999px;
            background: rgba(255, 255, 255, 0.05);
            border: 1px solid var(--card-border);
        }

        .status-dot {
            width: 8px;
            height: 8px;
            border-radius: 50%;
            background-color: var(--text-muted);
        }

        .status-dot.active {
            background-color: var(--accent);
            box-shadow: 0 0 10px var(--accent);
            animation: pulse 2s infinite;
        }

        .status-dot.pending {
            background-color: var(--accent-warn);
            box-shadow: 0 0 10px var(--accent-warn);
            animation: pulse-warn 2s infinite;
        }

        @keyframes pulse {
            0% { transform: scale(0.95); box-shadow: 0 0 0 0 rgba(16, 185, 129, 0.7); }
            70% { transform: scale(1); box-shadow: 0 0 0 8px rgba(16, 185, 129, 0); }
            100% { transform: scale(0.95); box-shadow: 0 0 0 0 rgba(16, 185, 129, 0); }
        }

        @keyframes pulse-warn {
            0% { transform: scale(0.95); box-shadow: 0 0 0 0 rgba(245, 158, 11, 0.7); }
            70% { transform: scale(1); box-shadow: 0 0 0 8px rgba(245, 158, 11, 0); }
            100% { transform: scale(0.95); box-shadow: 0 0 0 0 rgba(245, 158, 11, 0); }
        }

        .card {
            background: var(--card-bg);
            border: 1px solid var(--card-border);
            border-radius: 16px;
            padding: 1.5rem;
            backdrop-filter: blur(12px);
            box-shadow: 0 4px 30px rgba(0, 0, 0, 0.15);
            transition: border-color 0.3s;
        }

        .card:hover {
            border-color: rgba(16, 185, 129, 0.2);
        }

        .card-title {
            font-size: 1.1rem;
            font-weight: 600;
            margin-bottom: 1rem;
            display: flex;
            align-items: center;
            gap: 0.5rem;
            color: #ffffff;
        }

        .form-group {
            margin-bottom: 1.2rem;
            display: flex;
            flex-direction: column;
            gap: 0.5rem;
        }

        .form-group:last-child {
            margin-bottom: 0;
        }

        label {
            font-size: 0.85rem;
            font-weight: 600;
            color: var(--text-muted);
            text-transform: uppercase;
            letter-spacing: 0.05em;
        }

        input[type="text"], select, textarea {
            width: 100%;
            background: rgba(0, 0, 0, 0.2);
            border: 1px solid var(--card-border);
            border-radius: 8px;
            padding: 0.75rem 1rem;
            color: var(--text-main);
            font-family: inherit;
            font-size: 0.95rem;
            outline: none;
            transition: border-color 0.2s, box-shadow 0.2s;
        }

        input[type="text"]:focus, select:focus, textarea:focus {
            border-color: var(--accent);
            box-shadow: 0 0 0 3px var(--accent-glow);
        }

        .toggle-container {
            display: flex;
            justify-content: space-between;
            align-items: center;
        }

        .switch {
            position: relative;
            display: inline-block;
            width: 46px;
            height: 26px;
        }

        .switch input {
            opacity: 0;
            width: 0;
            height: 0;
        }

        .slider {
            position: absolute;
            cursor: pointer;
            top: 0; left: 0; right: 0; bottom: 0;
            background-color: rgba(255, 255, 255, 0.1);
            transition: .3s;
            border-radius: 34px;
            border: 1px solid var(--card-border);
        }

        .slider:before {
            position: absolute;
            content: "";
            height: 18px; width: 18px;
            left: 3px; bottom: 3px;
            background-color: white;
            transition: .3s;
            border-radius: 50%;
        }

        input:checked + .slider {
            background-color: var(--accent);
        }

        input:checked + .slider:before {
            transform: translateX(20px);
        }

        .btn {
            background: linear-gradient(135deg, #10b981 0%, #059669 100%);
            border: none;
            border-radius: 8px;
            color: white;
            font-family: inherit;
            font-weight: 600;
            padding: 0.85rem 1.5rem;
            cursor: pointer;
            transition: transform 0.1s, opacity 0.2s;
            display: flex;
            align-items: center;
            justify-content: center;
            gap: 0.5rem;
            width: 100%;
        }

        .btn:hover {
            opacity: 0.9;
        }

        .btn:active {
            transform: scale(0.98);
        }

        .qr-section {
            display: flex;
            flex-direction: column;
            align-items: center;
            gap: 1rem;
            padding: 2rem;
            text-align: center;
        }

        #qrcode {
            background: white;
            padding: 1rem;
            border-radius: 12px;
            box-shadow: 0 4px 20px rgba(0, 0, 0, 0.4);
        }

        .toast {
            position: fixed;
            bottom: 2rem;
            right: 2rem;
            background: #10b981;
            color: white;
            padding: 0.85rem 1.5rem;
            border-radius: 8px;
            font-weight: 600;
            box-shadow: 0 4px 15px rgba(0, 0, 0, 0.3);
            transform: translateY(150%);
            transition: transform 0.3s cubic-bezier(0.175, 0.885, 0.32, 1.275);
            display: flex;
            align-items: center;
            gap: 0.5rem;
            z-index: 1000;
        }

        .toast.show {
            transform: translateY(0);
        }

        .message-item {
            display: flex;
            flex-direction: column;
            gap: 0.25rem;
            background: rgba(0, 0, 0, 0.2);
            border: 1px solid var(--card-border);
            border-radius: 8px;
            padding: 0.75rem;
            transition: background-color 0.2s;
        }

        .message-item:hover {
            background: rgba(255, 255, 255, 0.02);
        }

        .message-header {
            display: flex;
            justify-content: space-between;
            align-items: center;
            font-size: 0.8rem;
            margin-bottom: 0.25rem;
        }

        .message-name {
            font-weight: 600;
            color: #ffffff;
        }

        .message-tag {
            font-size: 0.7rem;
            font-weight: 600;
            padding: 0.15rem 0.4rem;
            border-radius: 4px;
            text-transform: uppercase;
        }

        .message-tag.dm {
            background: rgba(16, 185, 129, 0.15);
            color: var(--accent);
        }

        .message-tag.group {
            background: rgba(59, 130, 246, 0.15);
            color: #3b82f6;
        }

        .message-body {
            font-size: 0.9rem;
            color: var(--text-main);
            word-break: break-word;
            line-height: 1.4;
        }

        .message-time {
            font-size: 0.75rem;
            color: var(--text-muted);
        }
    </style>
</head>
<body>
    <div class="container">
        <header>
            <div>
                <h1>Bot Dashboard</h1>
                <p style="font-size: 0.85rem; color: var(--text-muted); margin-top: 0.2rem;" id="jid-display">Disconnected</p>
            </div>
            <div class="status-badge">
                <span class="status-dot" id="status-dot"></span>
                <span id="status-text">Checking Status...</span>
            </div>
        </header>

        <!-- QR Code Card -->
        <div class="card" id="qr-card" style="display: none;">
            <div class="card-title">🔑 Connection Authentication</div>
            <div class="qr-section">
                <p style="color: var(--text-muted); font-size: 0.95rem;">Scan this QR code using WhatsApp on your phone to link your bot device:</p>
                <div id="qrcode"></div>
                <p style="font-size: 0.8rem; color: var(--accent-warn);">⚠️ Waiting for QR code to load...</p>
            </div>
        </div>

        <!-- Recent Messages Card -->
        <div class="card" id="messages-card">
            <div class="card-title">💬 Recent Messages Log</div>
            <div id="messages-list" style="max-height: 300px; overflow-y: auto; display: flex; flex-direction: column; gap: 0.75rem; padding-right: 0.5rem;">
                <p style="color: var(--text-muted); font-size: 0.9rem; text-align: center; padding: 1rem;">No messages logged yet...</p>
            </div>
        </div>

        <!-- Bot Configurations Card -->
        <div class="card" id="settings-card">
            <div class="card-title">⚙️ Bot Settings</div>
            
            <div class="form-group toggle-container" style="margin-bottom: 1.5rem;">
                <div>
                    <label style="font-size: 0.95rem; color: #ffffff;">Away Mode</label>
                    <p style="font-size: 0.8rem; color: var(--text-muted); margin-top: 0.1rem;">Automatically reply to messages when you are away</p>
                </div>
                <label class="switch">
                    <input type="checkbox" id="away_mode">
                    <span class="slider"></span>
                </label>
            </div>

            <div class="form-group toggle-container" style="margin-bottom: 1.5rem;">
                <div>
                    <label style="font-size: 0.95rem; color: #ffffff;">AI Assistant Active</label>
                    <p style="font-size: 0.8rem; color: var(--text-muted); margin-top: 0.1rem;">Automatically reply to DMs using Groq Llama AI</p>
                </div>
                <label class="switch">
                    <input type="checkbox" id="ai_assist">
                    <span class="slider"></span>
                </label>
            </div>

            <div class="form-group">
                <label for="away_message">Away Message</label>
                <textarea id="away_message" rows="2" placeholder="Enter custom away reply..."></textarea>
            </div>

            <div class="form-group">
                <label for="blocked_words">Blocked Words (Comma-Separated)</label>
                <input type="text" id="blocked_words" placeholder="spam, scam, promo, advertising">
            </div>

            <div class="form-group">
                <label for="groq_model">Groq Model Selector</label>
                <select id="groq_model">
                    <option value="llama-3.3-70b-versatile">llama-3.3-70b-versatile (Smart / Precise)</option>
                    <option value="llama-3.1-8b-instant">llama-3.1-8b-instant (Fast / Low Latency)</option>
                </select>
            </div>

            <button class="btn" style="margin-top: 1.5rem;" id="save-btn">
                💾 Save Dashboard Settings
            </button>
        </div>
    </div>

    <div class="toast" id="toast">
        ✅ Settings saved successfully!
    </div>

    <script>
        let qrRenderer = null;
        let lastQRString = "";

        async function fetchStatus() {
            try {
                const response = await fetch('/api/status');
                const data = await response.json();
                
                const dot = document.getElementById('status-dot');
                const text = document.getElementById('status-text');
                const jidDisplay = document.getElementById('jid-display');

                if (data.connected) {
                    dot.className = 'status-dot active';
                    text.innerText = 'Connected';
                    jidDisplay.innerText = "Logged in as: " + data.push_name + " (" + data.jid + ")";
                    document.getElementById('qr-card').style.display = 'none';
                } else {
                    dot.className = 'status-dot pending';
                    text.innerText = 'Needs Authentication';
                    jidDisplay.innerText = 'Waiting for WhatsApp login...';
                    document.getElementById('qr-card').style.display = 'block';
                    fetchQRCode();
                }
            } catch (err) {
                console.error("Error fetching status:", err);
            }
        }

        async function fetchQRCode() {
            try {
                const response = await fetch('/api/qr');
                const data = await response.json();
                
                const qrContainer = document.getElementById('qrcode');
                const warnText = qrContainer.nextElementSibling;

                if (data.connected) {
                    document.getElementById('qr-card').style.display = 'none';
                    return;
                }

                if (data.qr) {
                    warnText.style.display = 'none';
                    if (data.qr !== lastQRString) {
                        lastQRString = data.qr;
                        qrContainer.innerHTML = '';
                        qrRenderer = new QRCode(qrContainer, {
                            text: data.qr,
                            width: 200,
                            height: 200,
                            colorDark : "#000000",
                            colorLight : "#ffffff",
                            correctLevel : QRCode.CorrectLevel.H
                        });
                    }
                } else {
                    qrContainer.innerHTML = '';
                    warnText.style.display = 'block';
                    warnText.innerText = '⏳ Waiting for whatsmeow to generate a login QR code...';
                }
            } catch (err) {
                console.error("Error fetching QR code:", err);
            }
        }

        async function fetchConfig() {
            try {
                const response = await fetch('/api/config');
                const data = await response.json();
                
                document.getElementById('away_mode').checked = data.away_mode;
                document.getElementById('ai_assist').checked = data.ai_assist;
                document.getElementById('away_message').value = data.away_message || "";
                document.getElementById('blocked_words').value = (data.blocked_words || []).join(', ');
                document.getElementById('groq_model').value = data.groq_model || 'llama-3.3-70b-versatile';
            } catch (err) {
                console.error("Error fetching config:", err);
            }
        }

        async function fetchMessages() {
            try {
                const response = await fetch('/api/messages');
                const data = await response.json();
                const list = document.getElementById('messages-list');
                if (data && data.length > 0) {
                    list.innerHTML = '';
                    data.slice().reverse().forEach(msg => {
                        const item = document.createElement('div');
                        item.className = 'message-item';
                        
                        const header = document.createElement('div');
                        header.className = 'message-header';
                        
                        const nameSpan = document.createElement('span');
                        nameSpan.className = 'message-name';
                        const displayName = msg.push_name ? (msg.push_name + " (" + msg.sender.split('@')[0] + ")") : msg.sender.split('@')[0];
                        nameSpan.innerText = displayName;
                        
                        const rightPart = document.createElement('div');
                        rightPart.style.display = 'flex';
                        rightPart.style.alignItems = 'center';
                        rightPart.style.gap = '0.5rem';
                        
                        const tag = document.createElement('span');
                        tag.className = 'message-tag ' + (msg.is_group ? 'group' : 'dm');
                        tag.innerText = msg.is_group ? 'Group' : 'DM';
                        
                        const timeSpan = document.createElement('span');
                        timeSpan.className = 'message-time';
                        timeSpan.innerText = msg.timestamp;
                        
                        rightPart.appendChild(tag);
                        rightPart.appendChild(timeSpan);
                        
                        header.appendChild(nameSpan);
                        header.appendChild(rightPart);
                        
                        const body = document.createElement('div');
                        body.className = 'message-body';
                        body.innerText = msg.content;
                        
                        item.appendChild(header);
                        item.appendChild(body);
                        list.appendChild(item);
                    });
                }
            } catch (err) {
                console.error("Error fetching messages:", err);
            }
        }

        document.getElementById('save-btn').addEventListener('click', async () => {
            const awayMode = document.getElementById('away_mode').checked;
            const awayMessage = document.getElementById('away_message').value;
            const blockedWordsRaw = document.getElementById('blocked_words').value;
            const groqModel = document.getElementById('groq_model').value;
            const aiAssist = document.getElementById('ai_assist').checked;

            const blockedWords = blockedWordsRaw.split(',')
                .map(w => w.trim().toLowerCase())
                .filter(w => w !== "");

            const payload = {
                away_mode: awayMode,
                away_message: awayMessage,
                blocked_words: blockedWords,
                groq_model: groqModel,
                ai_assist: aiAssist
            };

            try {
                const saveBtn = document.getElementById('save-btn');
                saveBtn.disabled = true;
                saveBtn.innerText = 'Saving...';

                const response = await fetch('/api/config', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(payload)
                });
                
                if (response.ok) {
                    showToast();
                } else {
                    alert("Error saving configuration!");
                }
            } catch (err) {
                console.error("Error saving settings:", err);
            } finally {
                const saveBtn = document.getElementById('save-btn');
                saveBtn.disabled = false;
                saveBtn.innerText = '💾 Save Dashboard Settings';
            }
        });

        function showToast() {
            const toast = document.getElementById('toast');
            toast.classList.add('show');
            setTimeout(() => {
                toast.classList.remove('show');
            }, 3000);
        }

        // Initialize and poll status
        fetchStatus();
        fetchConfig();
        fetchMessages();
        setInterval(fetchStatus, 5000);
        setInterval(fetchMessages, 3000);
    </script>
</body>
</html>
`
