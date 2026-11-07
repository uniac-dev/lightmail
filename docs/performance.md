# Resource usage

The backend uses one Go process and an embedded store. One delivery worker
sleeps on a notification or the next retry deadline. The dashboard is static
Svelte output served by its own Go process. Neither production service runs
Node.js, a separate database, Redis, or an idle polling loop.

## Measured idle installation

Measured on macOS arm64 with Go 1.27.3, stripped native binaries, an empty
installation, and a 30-second observation after readiness:

| Component | Resident memory | Observed CPU increase |
| --- | ---: | ---: |
| Backend | 5.61 MiB | 0.00% |
| Dashboard HTTP server | 4.78 MiB | 0.00% |

The initial database occupied 128 KiB. Compiled dashboard files occupied
138,157 bytes, including dependency license notices. The JavaScript bundle was
about 34.6 kB compressed with gzip; CSS was about 5.8 kB compressed.

The [raw measurement](idle-darwin-arm64.json) records bytes and the observation
duration. CPU is derived from the operating system's cumulative process timer;
zero means no increase at that timer's resolution. These figures exclude browser
memory, container overhead, and reserved infrastructure capacity. They describe
an empty idle installation, not active delivery or a large message history.
Actual usage depends on the operating system, traffic, attachments, DNS and TLS.

Reproduce locally after building the dashboard:

    npm ci
    npm run build
    python3 scripts/measure_idle.py --seconds 30

The script builds both binaries, starts isolated loopback listeners with a
temporary store and random credential, measures their processes, and cleans
them up. It does not send email.

## Operating bounds

This version serializes recipient delivery through one worker. It favors small
installations and low idle usage over bulk throughput. A slow recipient can
delay the queue, with a bounded 90-second delivery context per recipient.
HTTP requests admit up to 50 recipients and 10 attachments, with a 15 MiB
request limit and a 10 MiB decoded attachment limit.

Email list pages read metadata, without loading message bodies. History defaults
to 30 days. Retention runs at startup and at most hourly on API activity, so an
idle installation does not wake solely to delete history. Free bbolt pages are
reused; the database file is not automatically compacted. Queued content and
signing keys remain until they are resolved or explicitly removed.

The Uniac deployment uses the platform's minimum 512 MiB allocation for each
service. A small process footprint does not reduce a platform's minimum
reservation or price.
