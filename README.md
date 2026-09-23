# GoAlert

GoAlert provides on-call scheduling, automated escalations and notifications (like SMS or voice calls) to automatically engage the right person, the right way, and at the right time.

![main-screen-updated](https://user-images.githubusercontent.com/595010/189744659-66ee6aed-b7b6-4625-a2ac-1f8ad3c1ea4f.png)

## About this fork

This is a fork of [target/goalert](https://github.com/target/goalert). It tracks upstream but carries changes that are not upstream, listed here so nobody has to reconstruct them from the commit log. Anything not in this table behaves as upstream does.

| Change | What it does | PR |
|---|---|---|
| Per-service notification urgency | A service set to **low** records alerts without paging anyone. The escalation policy never runs for them. | [#1](https://github.com/Radico/goalert/pull/1) |
| Per-alert assignment | Alerts track an owner. Unclaimed alerts show whoever is on-call, acknowledging claims the alert, and alerts can be re-assigned. Never affects notification routing. Gated on `General.EnableAlertAssignment`. | [#1](https://github.com/Radico/goalert/pull/1) |
| Alert comments | Free-form comments on an alert, for triage context that does not belong in the system-generated log. Always on. | [#2](https://github.com/Radico/goalert/pull/2) |
| Skip-if-empty escalation steps | A step that resolves to nobody escalates immediately instead of waiting out its delay. Per-step checkbox, off by default. | [#2](https://github.com/Radico/goalert/pull/2) |
| Schedule-based alerting windows | A service notifies only during configured weekly windows. Alerts captured outside a window are recorded and notify when the window next opens. Behind the `svc-alert-schedule` experimental flag. | [#3](https://github.com/Radico/goalert/pull/3) |
| Ownership resolves past empty steps | Ownership falls through to the earliest escalation step that has somebody on-call, instead of resolving against the current step only. Without this, low urgency and out-of-window alerts read as unassigned, since those never escalate off step 0. | [#4](https://github.com/Radico/goalert/pull/4) |
| Alerts from services you are on-call for | The home page covers every alert on services you are the primary on-call for, whoever holds them. It previously showed alerts that had *paged* you, which hid an open alert on your own service claimed by the previous rotation. Adds an opt-in filter for services you are further down the escalation path for, and `General.DisableOnCallServiceAlerts` to turn the whole thing off. | [#5](https://github.com/Radico/goalert/pull/5) |
| Row action menus are clickable | `CompListItemNav` rendered its action inside the row's link, so clicking the ellipsis followed the link. On schedule assignments that made it impossible to remove a user. Upstream defect — worth sending back. | [#6](https://github.com/Radico/goalert/pull/6) |
| Rotation active index on delete | Removing a rotation's active user while they were last in the list sent an index the rotation no longer had, and the server rejected it with `invalid index for rotation`. Upstream defect — worth sending back. | [#6](https://github.com/Radico/goalert/pull/6) |
| First staffed step is the primary on-call | A service whose first step reaches nobody belonged to nobody. Once somebody acknowledged such an alert, ownership froze to them and it disappeared from the list of whoever was actually responsible. | [#7](https://github.com/Radico/goalert/pull/7) |
| Filter ownership by source | An Ownership filter separating what was handed to you from what is yours by rotation and unclaimed, plus alerts owned by nobody at all. | [#9](https://github.com/Radico/goalert/pull/9) |

## Installation

GoAlert is distributed as a single binary with release notes available from the [GitHub Releases](https://github.com/target/goalert/releases) page.
Additionally, images are published on [Docker Hub](https://hub.docker.com/r/goalert/goalert) for each release. The `latest` tag is the most recent release, and `nightly` is the latest build from the `master` branch.

See our [Getting Started Guide](./docs/getting-started.md) for running GoAlert in a production environment.

### Quick Start

```bash
docker run -it --rm -p 8081:8081 goalert/demo
```

GoAlert will be running at [localhost:8081](http://localhost:8081). You can log in with `admin`/`admin123`.

If you're using the demo container for integration testing:

- A non-admin user is available as `user`/`user1234`.
- You can specify the ENV variable `SKIP_SEED=1` to skip the initial seed data step.
- You can get a session token via `curl -XPOST -H 'Referer: http://localhost:8081' -d 'username=admin&password=admin123' 'http://localhost:8081/api/v2/identity/providers/basic?noRedirect=1'`.

## Contributing (Local Development)

If you'd like to contribute to GoAlert, please see our [Contributing Guidelines](./CONTRIBUTING.md) and the [Development Setup Guide](./docs/development-setup.md).

Please also see our [Code of Conduct](https://github.com/target/.github/blob/main/CODE_OF_CONDUCT.md).

For most purposes, you can use `make start` from the root of this repo to start a development server.

- It will be running at `http://localhost:3030`
- Default login is `admin`/`admin123`
- Changes you make locally, UI and backend, should be reflected in the running server within a few seconds (no need to restart the server).

## Contact Us

If you need help or have a question, the `#goalert` Slack channel is available on [gophers.slack.com](https://gophers.slack.com/messages/goalert/).

To access Gophers Slack and the `#goalert` channel, you will need an invitation. You request one through the automated process here: https://invite.slack.golangbridge.org/

- Vote on existing [Feature Requests](https://github.com/target/goalert/issues?q=is%3Aopen+label%3Aenhancement+sort%3Areactions-%2B1-desc) or submit [a new one](https://github.com/target/goalert/issues/new)
- File a [bug report](https://github.com/target/goalert/issues)
- Report security issues to security@goalert.me

## License

GoAlert is licensed under the [Apache License, Version 2.0](./LICENSE.md).
