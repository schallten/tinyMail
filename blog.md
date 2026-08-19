# 1

**RFC 2822 format** is a standard format used for **Internet email messages**. It defines how email headers and message content should be structured.

In short, an RFC 2822 email message has:

1. **Headers** (metadata about the email)

   * `From:` — sender's email address
   * `To:` — receiver's email address
   * `Date:` — date and time sent
   * `Subject:` — email subject
   * `Message-ID:` — unique message identifier

2. **Blank line**

   * Separates headers from the email body.

3. **Body**

   * The actual message text.

**Example:**

```
From: alice@example.com
To: bob@example.com
Date: Mon, 10 Aug 2026 15:00:00 +0530
Subject: Meeting Reminder
Message-ID: <12345@example.com>

Hello Bob,
This is a reminder for our meeting.
```


#2



**Why `os.ReadDir` instead of `filepath.Glob`?**
`ReadDir` reads directory entries directly from the filesystem metadata. `Glob` does pattern matching which requires scanning and filtering. For listing all `.eml` files, `ReadDir` + manual filter is faster and more predictable. Also, `Glob` can behave unexpectedly with special characters in filenames.


**Why ignore `os.IsNotExist` in DeleteMessage?**
Race condition protection. Two POP3 sessions could mark the same message for deletion. The first `QUIT` deletes it successfully. The second `QUIT` should not fail — the desired end state (message gone) is already achieved. Failing would confuse the client.


#3

Why send greeting in constructor, not in Run()?
RFC 5321 §4.2 mandates the server speaks first upon connection. Putting it in the constructor guarantees it happens exactly once, before any read attempt. If it were in Run(), a read timeout could theoretically fire before the greeting is sent.

Why iota for states instead of strings?
Integer comparison is O(1) and cache-friendly. String comparison allocates and is slower. In a hot loop processing every command, this matters. iota also prevents typos — the compiler catches invalid state values.

Why TrimRight instead of TrimSuffix("\r\n")?
Defensive parsing. Some broken clients send only \n without \r. TrimRight handles both cases. TrimSuffix would leave a stray \r if the client omitted it, causing silent parsing failures later.

#4

Normally, sending an email is a slow, back-and-forth game of phone tag. The client says "Here is the sender," and waits for a reply. Then it says "Here is the receiver," and waits again.Pipelining completely fixes this lag. It is like ordering a full meal at a drive-thru in one breath: "I want a burger, fries, and a drink."Instead of waiting for the server to reply to every single line, the client blasts a whole batch of commands over the network all at once. The server processes them sequentially, saving a massive amount of network time.

#5

Why does RSET return to GREETED instead of INIT?
RSET aborts the current message transaction, not the entire session. The client has already identified itself via EHLO. Forcing re-EHLO after every RSET would waste round-trips and violate RFC 5321 §4.1.1.5. Only a new TCP connection requires EHLO.

Why check line length BEFORE TrimRight?
If a client sends 2000 bytes without \n, ReadString blocks until timeout. But if they send 2000 bytes with \n, we receive it immediately. Checking len(line) before trimming catches both cases: oversized valid lines AND lines that happen to include CRLF within the limit but exceed it overall. Trimming first would hide the true wire size.

What happens iif a client sends a 1500-byte line ending with \r\n? (Answer: Rejected with 500, connection closed)


## SMTP summary

When an email arrives, it doesn’t go straight to the inbox. It goes to a temporary “staging” folder first.
Only after the entire email is safely written to disk does the system move it to the real inbox in one atomic step.
If the server crashes mid-write, the inbox stays clean. No half-written, corrupted emails ever reach the user.
Each user has their own isolated folders. Alice can never accidentally see Bob’s mail.

SMTP Engine

t follows a strict conversation script (state machine). Clients must identify themselves → declare sender → declare recipients → send body → confirm. Out-of-order commands are rejected.
It speaks the universal email language (RFC 5321) correctly: proper response codes, dot-stuffing for body safety, line length limits to prevent attacks.
It streams large emails directly to the filing system instead of holding them in memory. A 100MB attachment won’t crash the server.
It handles multiple messages per connection efficiently, and resets cleanly between transactions.
Idle or malicious clients are automatically disconnected after 30 seconds of silence.


# POP 3
##1
Why store password temporarily between USER and PASS?
POP3 requires USER then PASS as two separate commands. There’s no way to validate credentials atomically. Storing the username in user and waiting for PASS is the only option. If PASS never arrives (timeout/disconnect), the stored username is discarded when the session ends. Never log or persist this value.



