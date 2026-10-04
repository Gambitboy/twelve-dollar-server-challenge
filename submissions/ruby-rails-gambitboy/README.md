# Ruby + Rails

| | |
|---|---|
| Language | Ruby 4.0.5, built from source with YJIT |
| Framework | Rails 8.1.4 (full Rails: Active Record, Action Controller, router) |
| Server | Puma 8.0.2 |
| SQLite driver | sqlite3 gem 2.9.6 (bundled SQLite 3.53.2) |
| JWT | jwt gem 3.3.0 |
| **Nginx or direct** | **Direct**: serves `0.0.0.0:80` itself |

All gem versions, transitive ones included, are pinned in `Gemfile.lock`.

## Running it

```bash
sudo bash install.sh   # apt build deps + tzdata, compiles Ruby 4.0.5 into /opt/ruby
bash build.sh          # bundle install into vendor/bundle, bootsnap precompile
SQLITE_PATH=... JWT_SECRET=... HOST=0.0.0.0 PORT=80 bash start.sh
```

`script/bench <VUS>` runs `bench/load.js` locally: the server in an Ubuntu 24.04 container pinned to one
core with 2 GB and no swap, and k6 from the `grafana/k6` image. `DURATION` and `RAMP_UP` shorten a run.

## How it's built

- **Active Record models** `User`, `Post` and `Like` map the fixed schema. `Like` uses a composite primary
  key. Rails' own timestamps are off, so `created_at` comes from the column default and is read back with
  `RETURNING`.
- **Post reads** are one query: `Post.with_details` joins the author and selects `like_count` as a
  correlated subquery.
- **Likes** are one `INSERT ... ON CONFLICT DO NOTHING RETURNING post_id` through `Like.insert`. A returned
  row means 201, none means 200.
- **Validation** lives on `Post`: `normalizes` trims the body, `validates` carries the spec's messages.
- **JWT** is verified on every request with the jwt gem, HS256 only.
- **Pragmas** are Rails 8's SQLite defaults: `journal_mode=WAL`, `synchronous=NORMAL`, `foreign_keys=ON`,
  128 MiB `mmap_size`.
- **Puma** runs in single mode with 3 threads and a 75 s keep-alive timeout.
- **YJIT** is on (`RUBY_YJIT_ENABLE=1`).

## License

MIT, under the repo's [license](../../LICENSE).
