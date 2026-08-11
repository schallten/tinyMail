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

