# tinyMail
making my own email and stuff

## the plan
storage/
├── alice/
│   ├── tmp/                    # Atomic staging area
│   ├── inbox/                  # Final, committed messages
│   └── meta/                   # Optional: flags, quotas, timestamps
│
├── bob/
│   ├── tmp/
│   ├── inbox/
│   └── meta/
│
└── .config                     # Global server metadata (optional)
    ├── users.db               # Plaintext or hashed user credentials
    └── quotas.json            # Per-user storage limits


## file naming convention
{TIMESTAMP}_{SEQUENCE}_{HASH}.eml

Example: 1722973920_001_a1b2c3.eml
         ├─ TIMESTAMP: Unix seconds (sorts chronologically)
         ├─ SEQUENCE: Client session order (handles same-second collisions)
         └─ HASH: First 6 chars of SHA-256(from+to+subject) (collision safety)

## atomic write
┌──────────────────────────────────────────────────────────┐
│              SMTP DATA Completion Flow                    │
└──────────────────────────────────────────────────────────┘

1. OPEN TEMP FILE
   fd = open("storage/alice/tmp/{uuid}.tmp", O_WRONLY | O_CREAT)

2. STREAM INCOMING BODY (with dot-stuffing removal)
   while not at terminator:
       write(fd, line)

3. FSYNC TO DISK (force buffer flush)
   fsync(fd)
   close(fd)

4. ATOMIC RENAME TO INBOX
   rename("storage/alice/tmp/{uuid}.tmp", 
          "storage/alice/inbox/{timestamp}_{seq}_{hash}.eml")
   
   ← This is atomic on POSIX systems.
     If server crashes mid-rename, file stays in tmp/
     and POP3 never sees a corrupted message.

5. RESPOND TO CLIENT
   SMTP Server: "+250 Message queued"
   
            