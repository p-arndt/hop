---
id: hostkeys
title: Host keys and authentication
nav: Host keys & auth
group: Navigation mode
label: Navigation mode
---

An unknown host key **aborts the dial** and shows a fingerprint card. [[y]] trusts it and
retries, appending it to your usual `~/.ssh/known_hosts`; [[n]] or [[esc]] trusts nothing. A
*mismatch* — a key that changed on a host you already know — is always a hard error, never a
prompt.

When the host asks for a password or a one-time code, a card opens by itself: [[enter]]
submits or moves to the next question, [[tab]]/[[shift+tab]] move between fields, [[ctrl+u]]
clears, and [[esc]] cancels the connect. The dial **waits inside the handshake** rather than
restarting, because a one-time code is only good once — and you are asked once per host,
since shells, SFTP, editors and tunnels all ride that one connection. Nothing is stored.
