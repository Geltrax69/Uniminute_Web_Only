#!/usr/bin/env python3
"""Tiny chatbot CLI for the rewind.ai-style /chat/completions endpoint.

Run it, paste your key when asked (hidden input), then chat.
No deps beyond stdlib.
"""
import getpass
import json
import urllib.error
import urllib.request

URL = "https://api.rewind.ai/v1/chat/completions/"
MODEL = "openai/gpt-6-astra-pro"


def ask(api_key, messages):
    body = json.dumps({"model": MODEL, "messages": messages}).encode()
    req = urllib.request.Request(
        URL,
        data=body,
        headers={
            "Authorization": f"Bearer {api_key}",
            "Content-Type": "application/json",
        },
    )
    try:
        with urllib.request.urlopen(req, timeout=60) as resp:
            data = json.loads(resp.read())
    except urllib.error.HTTPError as e:
        raise RuntimeError(f"HTTP {e.code}: {e.read().decode(errors='replace')}") from None
    return data["choices"][0]["message"]["content"]


def main():
    api_key = getpass.getpass("API key (hidden): ").strip()
    if not api_key:
        print("No key entered, exiting.")
        return

    messages = []
    print(f"Connected. Model: {MODEL}. Type 'exit' to quit.\n")

    # quick connectivity check
    messages.append({"role": "user", "content": "Hello"})
    try:
        reply = ask(api_key, messages)
        print(f"assistant> {reply}\n")
        messages.append({"role": "assistant", "content": reply})
    except Exception as e:
        print(f"Test call failed: {e}")
        return

    while True:
        try:
            user_input = input("you> ").strip()
        except (EOFError, KeyboardInterrupt):
            break
        if user_input.lower() in ("exit", "quit"):
            break
        if not user_input:
            continue
        messages.append({"role": "user", "content": user_input})
        try:
            reply = ask(api_key, messages)
        except Exception as e:
            print(f"Error: {e}")
            messages.pop()
            continue
        print(f"assistant> {reply}\n")
        messages.append({"role": "assistant", "content": reply})


if __name__ == "__main__":
    main()
