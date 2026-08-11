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

