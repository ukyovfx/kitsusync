# Workflow Diagnosis

`/bot/admin/workflow-diagnosis` is a read-only diagnosis page for one Kitsu Production.

The page reads the selected Production, its Task Types and Task Statuses, global reference data, and current KitsuSync `ProjectWebhook` rows. It compares those values with the existing `cg` Discord routing template and labels exact matches as `Ready`, `Unrouted`, `Global only`, `Missing`, or `Ambiguous`. Similar names are informational suggestions only.

No Kitsu workflow provisioning is implemented here. The page does not create or update Kitsu data, Discord resources, webhooks, channels, users, or KitsuSync schema. Routing uses stable Production IDs and Task Type IDs; names are display metadata only. Stale references fail closed and are never guessed or remapped.

If exactly one Production exists it is selected automatically. When multiple Productions exist, use `?project=<kitsu-production-id>` to select one explicitly. If the Kitsu runtime is disconnected, the page stays available and shows a reconnect requirement instead of attempting a write or fallback operation.
