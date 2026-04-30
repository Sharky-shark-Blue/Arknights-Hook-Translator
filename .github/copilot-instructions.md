# Project Guidelines — 明日方舟日服文本捕获与汉化工具

## Project Overview

This tool captures in-game Japanese UI text from **Arknights JP** (`com.YoStarJP.Arknights`) on a rooted Android device using Frida, then displays/records translations in real time on a Windows PC.

**Pipeline:**
```
Android game (Unity IL2CPP)
  → Frida hook (ui_text_hook.js) injects into game process
  → Unicode-escaped text streamed over ADB
  → Go binary (start_fanyi.exe) decodes, deduplicates, looks up trans.json
  → Chinese translation shown in console
```

## Architecture

| Component | File | Role |
|-----------|------|------|
| Frida hook | `ui_text_hook.js` | IL2CPP string intercept, async queue, dedupe, unicode output |
| Go orchestrator | `start_fanyi.go` | ADB management, frida-server lifecycle, logcat parsing, trans.json I/O |
| Auto-inject | `auto_inject_uid.ps1` | Watches for game PID changes and re-injects the hook |
| Re-inject shortcut | `start_reinject.bat` | One-click manual re-injection |
| Translation DB | `trans.json` | `{ "日本語原文": "中文译文" }` — keys may include Unity rich-text tags |
| ADB runtime | `platform-tools/` | Bundled ADB; prefer this over system ADB |
| Frida server | `frida-server-16.1.2-android-arm64` | Must be pushed to device and version-matched to host frida tools |

## Build & Run

```powershell
# Build the Go binary (output already committed)
go build -o start_fanyi.exe start_fanyi.go

# Run the main tool (starts frida-server, injects hook, streams translations)
.\start_fanyi.exe

# Manual re-inject if the hook dies mid-session
.\start_reinject.bat

# Auto re-inject on PID change (keep in a separate shell)
.\auto_inject_uid.ps1
```

No external Go modules — the project uses only the standard library.

## Text-Filtering Convention

Both `ui_text_hook.js` and `start_fanyi.go` apply the **same filter rules** before outputting or saving text. When adding new filter logic, keep both files in sync:

| Pattern | Regex example | Action |
|---------|---------------|--------|
| Pure numbers | `^\d+$` | Skip |
| Slash-number | `^/\d+$` | Skip |
| Number pair | `^\d+/\d+$` | Skip |
| Percentage | `^\d{1,3}%$` | Skip |
| JP time text | `^\d+[日時間分秒]+$` | Skip |
| English/symbols only | `^[A-Za-z0-9_./:\- ]+$` | Skip |
| No JP/Han chars | no `[\p{Han}\x3040-\x30ff]` | Skip |

Text that passes all filters is saved to `trans.json` with itself as placeholder value.

## trans.json Key Format

- Keys are **original game strings**, possibly containing Unity rich-text tags (`<size=21>`, `<color=#fff>`, etc.).
- Values are Chinese translations; untranslated entries have the original text as value.
- The file is loaded once at startup and **auto-saved every 10 seconds** when dirty.
- Atomic write: Go writes to `trans.json.tmp` then renames — never edit the file while the tool is running.

## Frida / ADB Notes

- `frida-server-16.1.2-android-arm64` must be deployed to `/data/local/tmp/` on device and match the version of host `frida-inject`/`frida-tools`.
- ADB path preference: bundled `platform-tools\adb.exe` is used if present, else falls back to system `adb`.
- The game process may have **multiple PIDs** (app + service); `auto_inject_uid.ps1` always takes the first one from `pidof`.
- Crash logs from failed hook sessions are saved to `crash_logs/` with paired `_key.txt` files.

## IL2CPP String Layout (ui_text_hook.js)

Unity version determines the `System.String` field offsets:

| Unity version | Length offset | Chars offset |
|---------------|---------------|--------------|
| 2019–2021 | `0x10` | `0x14` |
| 2022+ | `0x14` | `0x18` |

The hook tries both layouts. When the game updates, verify offsets haven't shifted.

## Conventions That Differ From Defaults

- **No test files** — the project is a runtime tool; verify by running against the device.
- **Crash files are evidence** — do not delete `crash_logs/`. Analyse `_key.txt` (hook config) alongside the crash dump.
- **Console output** is intentionally minimal (cleared per new text) to keep the translator's focus on the latest string.
- All user-facing strings in Go are in **Simplified Chinese** (`[ADB] 使用:`, `[ERROR] 缺少文件:`, etc.).
