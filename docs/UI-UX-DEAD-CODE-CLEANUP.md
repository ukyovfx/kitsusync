# KitsuSync UI Dead-Code Cleanup

## Scope

この整理では、現行の描画UIから到達不能であることを参照検索で確認できた実装だけを削除しました。既存のdirty worktree変更と外部状態は保持しています。

## Removed

- 未使用のSystem Status旧refresh script群: systemStatusRefreshScript、systemStatusRefreshScriptSharedScale、systemStatusRefreshScriptReadableLegacy、systemStatusRefreshScriptReadableRaw
- 未使用のbar-era renderer: apiObservationBarGraphWithScaleRaw、apiObservationBarGraphIndexed
- markup/runtime参照のない `.workflow-support-card` selector
- renderIAHealth内で直後に現行readable scriptへ置換されていた旧bar-chart refresh payload。置換経路は最小markerで維持

各項目は削除前にrepository-wide参照検索を行い、active call siteまたはemitted class referenceがないことを確認しました。現行line sparkline rendererとinteraction scriptは保持しています。

## Intentionally retained

- compatibilityまたはcurrent test参照が残るLegacy connection / Production settings helper
- 動的markupやresponsive/current markupで使われる共有CSSとtoken
- current testが実行するtest-only compatibility helper
- uncertainなpage-local CSS候補。推測による削除はしていません。

## Validation

- Focused telemetry/UI tests: PASS
- CGO-enabled setup tests in Docker: PASS
- go vet ./src/setup: PASS
- gofmt: PASS
- git diff --check: PASS
- docker compose config --quiet: PASS
- Docker rebuild/recreate: PASS
- /health: HTTP 200
- External Kitsu/Discord writes: none
