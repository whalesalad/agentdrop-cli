#!/usr/bin/env python3
"""Drive the win11-agentdrop VM: screenshots, mouse via QMP, keyboard via send-key."""
import json, os, subprocess, sys, time
DOM = "win11-agentdrop"
CONN = ["virsh", "-c", "qemu:///system"]
S = os.environ.get("VM_SHOTS", os.path.expanduser("~/vm-shots"))
os.makedirs(S, exist_ok=True)
W, H = 1280, 800

def virsh(*args, check=True):
    return subprocess.run(CONN + list(args), capture_output=True, text=True, check=check)

def qmp(cmd, **arguments):
    payload = json.dumps({"execute": cmd, "arguments": arguments})
    r = virsh("qemu-monitor-command", DOM, payload)
    return r.stdout

def shot(name="shot"):
    ppm = f"{S}/{name}.ppm"; png = f"{S}/{name}.png"
    virsh("screenshot", DOM, ppm)
    subprocess.run(["convert", ppm, png], check=True)
    print(png)

def geometry():
    """Detect current guest resolution from a screenshot header."""
    global W, H
    ppm = f"{S}/_geo.ppm"
    virsh("screenshot", DOM, ppm)
    import struct
    with open(ppm, "rb") as f:
        head = f.read(64)
    if head.startswith(b"\x89PNG"):
        W, H = struct.unpack(">II", head[16:24])
    else:
        tokens = head.split()[1:3]
        W, H = int(tokens[0]), int(tokens[1])
    return W, H

def move(x, y):
    geometry()
    ax = int(x * 32767 / max(W - 1, 1)); ay = int(y * 32767 / max(H - 1, 1))
    qmp("input-send-event", events=[{"type": "abs", "data": {"axis": "x", "value": ax}},
                                     {"type": "abs", "data": {"axis": "y", "value": ay}}])

def click(x, y, button="left", n=1):
    move(x, y); time.sleep(0.15)
    for _ in range(n):
        qmp("input-send-event", events=[{"type": "btn", "data": {"down": True, "button": button}}])
        time.sleep(0.05)
        qmp("input-send-event", events=[{"type": "btn", "data": {"down": False, "button": button}}])
        time.sleep(0.12)

SHIFT = {'!': '1', '@': '2', '#': '3', '$': '4', '%': '5', '^': '6', '&': '7', '*': '8', '(': '9', ')': '0',
         '_': 'minus', '+': 'equal', '{': 'leftbrace', '}': 'rightbrace', '|': 'backslash', ':': 'semicolon',
         '"': 'apostrophe', '<': 'comma', '>': 'dot', '?': 'slash', '~': 'grave'}
PLAIN = {'-': 'minus', '=': 'equal', '[': 'leftbrace', ']': 'rightbrace', '\\': 'backslash', ';': 'semicolon',
         "'": 'apostrophe', ',': 'comma', '.': 'dot', '/': 'slash', '`': 'grave', ' ': 'space', '\n': 'enter', '\t': 'tab'}

def keyname(ch):
    if ch.isalpha():
        return (["KEY_LEFTSHIFT"] if ch.isupper() else []) + [f"KEY_{ch.upper()}"]
    if ch.isdigit():
        return [f"KEY_{ch}"]
    if ch in SHIFT:
        return ["KEY_LEFTSHIFT", f"KEY_{SHIFT[ch].upper()}"]
    if ch in PLAIN:
        return [f"KEY_{PLAIN[ch].upper()}"]
    raise ValueError(f"unmapped char {ch!r}")

def keys(*names, hold=40):
    virsh("send-key", DOM, "--holdtime", str(hold), *names)

def type_text(text, delay=0.03):
    for ch in text:
        keys(*keyname(ch)); time.sleep(delay)

if __name__ == "__main__":
    cmd, args = sys.argv[1], sys.argv[2:]
    if cmd == "shot": shot(*args)
    elif cmd == "click": click(int(args[0]), int(args[1]), *(args[2:3] or ["left"]), n=int(args[3]) if len(args) > 3 else 1)
    elif cmd == "dbl": click(int(args[0]), int(args[1]), n=2)
    elif cmd == "move": move(int(args[0]), int(args[1]))
    elif cmd == "key": keys(*args)
    elif cmd == "type": type_text(args[0])
    elif cmd == "geo": print(geometry())
    elif cmd == "state": print(virsh("domstate", DOM).stdout.strip())
    else: sys.exit("unknown command")
