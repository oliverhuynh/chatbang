# ⚡ Chatbang Pro

> **ChatGPT from your terminal** — the full web experience. No API key. No quotas. No limits.

Chatbang Pro automates the official [ChatGPT](https://chatgpt.com) app in Chrome — every model, custom GPT, and feature OpenAI ships, from a fast, scriptable CLI.

| | |
|:---:|:---:|
| 🔑 **No API key** | 🆓 **No quotas** |
| 🧠 **Full ChatGPT** | ⚡ **Terminal-native** |
| 🛡️ **Stable sessions** | 🌍 **Unicode & RTL** |

Enhanced, actively maintained fork of [chatbang](https://github.com/ahmedhosssam/chatbang) — built for power users who want reliability, long replies, and headless operation.

---

## 📦 Installation

Download and install from the **[Releases](https://github.com/KaraBala10/chatbang-pro/releases)** page.

## 📦 Install from source:

```
git clone git@github.com:ahmedhosssam/chatbang.git
cd chatbang
go mod tidy
go build ./cmd/chatbang-pro/main.go
sudo mv main /usr/bin/chatbang
```

## 🌐 Requirements

**Google Chrome** must be installed. On Debian/Ubuntu amd64:

```bash
curl -fL https://dl.google.com/linux/direct/google-chrome-stable_current_amd64.deb \
  -o /tmp/google-chrome-stable_current_amd64.deb

sudo apt install /tmp/google-chrome-stable_current_amd64.deb
```

## ⚙️ Setup

```bash
chatbang-pro --config
```

Log in to ChatGPT in the browser window, then press **Enter** in the terminal.

Optional config at `$HOME/.config/chatbang/chatbang`:

```
browser=/usr/bin/google-chrome
headless=true
```

## 💬 Usage

```bash
chatbang-pro              # 🚀 start chat
chatbang-pro --config     # 🔐 refresh login
chatbang-pro -g g-XXXX    # 🎯 custom GPT (full URL or g-... id)
chatbang-pro --help       # 📖 full CLI reference (-h)
```

Type `exit` or `quit` to leave. Run `chatbang-pro --help` for all flags and options.

---

<p align="center">
  <strong>Built for the terminal. Powered by ChatGPT.</strong> 🖥️✨
</p>
