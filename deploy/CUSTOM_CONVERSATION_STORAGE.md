# Conversation Storage Deployment

This fork keeps the upstream Compose topology and overlays only the application
image so that local custom code is built and deployed.

## Deploy

```bash
cd deploy
cp .env.example .env
docker compose -f docker-compose.yml -f docker-compose.custom.yml up -d --build
```

Database migration `221_conversation_storage.sql` is applied automatically at
startup. The admin page is available at `/admin/conversations`.

The default retention policy is:

- successful raw request/response: 400 days
- failed raw request/response: 180 days
- normalized conversation text: 730 days
- session/request metadata: retained until an administrator deletes the session

Raw payloads are stored byte-for-byte after transport capture and compressed
with zstd. To keep a slow or unusually large stream from exhausting application
memory, each request and response has a default 32 MiB capture limit. Records
that exceed the limit remain available with `request_truncated` or
`response_truncated` set. Increase `CONVERSATION_STORAGE_MAX_REQUEST_BYTES` and
`CONVERSATION_STORAGE_MAX_RESPONSE_BYTES` only after checking the container
memory limit and peak concurrency.

Change the `CONVERSATION_STORAGE_*` values in `.env` before deployment when the
VPS disk budget requires shorter retention.

## Sync Upstream

Configure the original project once:

```bash
git remote add upstream https://github.com/Wei-Shaw/sub2api.git
```

Then update this fork through a dedicated integration branch:

```bash
git fetch upstream
git switch -c sync-upstream-YYYYMMDD
git merge upstream/main
docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.custom.yml build
```

After verification, merge the integration branch into the fork's main branch.
Do not edit applied migration `221_conversation_storage.sql`; add a new migration
for later schema changes.

## Rollback

Before rebuilding, tag the currently deployed custom image:

```bash
docker tag arcadia-ai/sub2api:custom arcadia-ai/sub2api:rollback-YYYYMMDD
```

To roll back application code, set the `image` in the override file to that tag
and run Compose without `--build`. Database metadata and archived conversations
remain compatible because migrations are additive.
