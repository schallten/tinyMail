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



