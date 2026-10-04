# モバイル対応の既知の制限

モバイル対応は未完了（実機ゲート未実施）。C14の自動試験はHeadless Chromiumによる検証であり、Android Chrome実機の代替ではありません。

## 実機で未検証の項目

[実機ゲート記録](phase0-device-report.md)の試験は未実施です。編集方式は `undecided` のまま既存インライン編集を維持し、限定ブロック編集は追加していません。hidden時のSSE切断は `no`、`interactive-widget` は `default`、タッチ行高は `adopt` を維持しています。

| 対象 | 未検証の内容 | 自動試験で確認する範囲 |
|---|---|---|
| IME | Gboardのフリックとローマ字、再変換、音声入力、連続削除、候補欄、行移動時の入力モード維持 | CDP合成、確定とUndo、合成イベントの順序。合成イベントにはブラウザの既定動作がありません。 |
| 文字範囲 | 長押し、選択ハンドル、標準選択メニューとの干渉 | DOM Selectionで指定した範囲と保存された文字列、列位置 |
| キーボードと表示 | 実際のソフトキーボード、アドレスバー、分割画面、フローティングキーボード、最大文字サイズ、端末の回転 | viewport寸法の切り替え、偽のvisualViewport、CDPによるsafe-area値の上書き |
| ライフサイクル | 5分のバックグラウンド、ロック解除、OSによるプロセス終了、端末ストレージの回収 | 合成visibilitychange、再読み込み、既存試験の永続ブラウザコンテキスト再起動 |
| 接続 | Wi-Fiとモバイル回線の切り替え、Discordリンク、実端末の名前解決、HTTPS経路、Cookieの到達性 | ローカル試験サーバーのSSE、HTTP失敗、通信切断の模擬 |
| 性能 | 指スクロール、GPU、発熱、省電力時の遅延 | CPU 4倍制限、3000行、プログラムによるスクロールとキー入力。測定値は [性能レポート](perf.md)に記録します。 |

端末下書きは同じブラウザの同じオリジンに保存されます。オフラインでレビュー全体を利用する機能、PWA、Service Worker、Web Pushは対象外です。Cookie、認証、ネットワーク設定はC14で変更していません。接続手順と通知URLの注意事項は [README](../../README.md)を参照してください。

## 試験の範囲と例外

Acceptanceは `mobile-360` と `mobile-412` の縦横、tap-targetsは両モバイル幅と `tablet-768` を対象とします。横向きの800pxはmedium、915pxはexpandedです。600px以上のプレビューは左右分割になり、840px以上では常設サイドバーになります。

横スクロールはnarrowのコード領域内だけを許容します。tap-targetsは横スクロール可能な `.code` 内にある要素の画面外位置を許容しますが、44×44pxの寸法判定には例外を設けません。浮動小数点の丸め誤差を考慮した下限は43.5pxです。43pxの試験用ボタンをコード内に挿入し、小さいボタンを検出できることも確認します。危険操作と同じコンテナの主操作などとの間隔は8px以上で検査します。

GitダイアログとAI確認ダイアログは、既存のAPI応答形式に合わせた表示用フィクスチャを使います。Gitリポジトリの作成やGit操作、エージェントへの依頼は実行しません。Gitの実処理を含めた実機受け入れ試験には相当しません。

既存の `viewport.e2e.ts` にはtablet-landscapeで既知のflakyがあります。C14ではテストも製品実装も変更せず、今回の実行結果を下記に記録します。expanded touchで長い送信先名を含むヘッダーが広がる既存の観測は [C11報告](c11-report.md#検証過程で検出した問題と対応)を参照してください。

## 自動受け入れ条件の対応

| 統合仕様の自動テスト | 試験ファイル |
|---|---|
| 360×800と412×915の全ルートのdocument幅 | `mobile/acceptance.e2e.ts`、既存 `mobile/overflow.e2e.ts` |
| 1440、1024、840pxの外観と操作 | 既存 `desktop/visual.e2e.ts`、`styles.e2e.ts`、`interactions.e2e.ts`、`boundary/layout.e2e.ts` |
| ドロワー、シート、プレビュー、行と文字範囲、Composer保存失敗 | `mobile/acceptance.e2e.ts`、`tap-targets.e2e.ts`、既存の各mobile試験 |
| SSE再接続、世代逆転、復帰同期、編集基準保持 | 既存 `sse/resync.e2e.ts`、`mobile/acceptance.e2e.ts` |
| 差分確認、競合拒否、改行とBOMの保持 | 既存 `desktop/edit.e2e.ts`、`mobile/edit-drafts.e2e.ts`、`mobile/ime.e2e.ts`、`mobile/acceptance.e2e.ts` |
| タップ領域と危険操作の間隔 | `mobile/tap-targets.e2e.ts`、`perf/perf.e2e.ts` |

C14はテストと文書だけを変更します。既存の画像と計算済みスタイルの基準を更新せず、製品コードとdistの差分がないことを確認します。API、ハッシュルート、localStorageキーの変更もありません。

## C14で検出した未解決事項

915×412のexpanded touchで、GitとAIの操作を同時に表示するフィクスチャはヘッダーが画面幅を超えます。プロジェクト名 `test-…`、ブランチ `fixture`、エージェント名 `Fixture`、送信先未選択、提出待ちコメント1件の状態で、設定した幅915pxに対してinnerWidthが924pxになりました。Gitダイアログ表示時のスクリーンショットでもヘッダー操作の重なりが見られます。

document幅とinnerWidthだけを比較すると、ブラウザがレイアウトviewportを広げた状態を見逃します。C14ではinnerWidthと指定viewport幅の一致も検査し、この状態を失敗として残しています。表示の検査はsoft assertionで記録し、後続の状態の画像と結果も収集します。期待値の緩和、skip、基準画像の更新は行いません。

この現象を解消するには製品のexpanded touchレイアウトを検討する必要があるため、C14の範囲では修正しません。C14の受け入れは未完了です。製品コード変更が必要な場合は停止するというユーザー指示に従い、許可範囲内の試験修正と結果の記録までで止めます。

指示書のC14は「6. Phase 5」にあります。依頼中の「9.4」に対応する節は現行指示書にはありません。対象ファイルは依頼で列挙された6ファイルに限定しました。

## C14の検証環境

作業対象は `feat/mobile-responsive`、開始時のHEADは `b72391b` で、作業ツリーは空でした。指定された指示書全体と統合仕様を確認しました。C14ではPhase 5の自動受け入れ試験と性能記録を追加しています。

Go検証は `GOCACHE=/tmp/reviewer-e2e-go-cache GOMODCACHE=/tmp/reviewer-e2e-go-mod` を指定しました。E2Eは既存ハーネスによって `XDG_CONFIG_HOME`、`XDG_STATE_HOME`、`XDG_DATA_HOME` を試験用root配下へ向けています。既存desktop画像とパスマスクを揃えるため、通常の回帰試験にはデフォルトの `/tmp/reviewer-e2e` を使っています。ビルド成功後のE2Eには `E2E_SKIP_BUILD=1` を指定しました。

`npm run e2e:sse` は既存のpackage.jsonに定義がなく、指定コマンドとしては失敗しました。変更の許可は `e2e:perf` に限られるため、SSEスクリプトを追加せず、同等の `npx playwright test --project=sse` を実行しました。

新規試験の作成中には、履歴画面で通常ファイル用の行IDを待っていた箇所、絵文字を含むボタン名、Composer保存と提出後ダイアログの待機条件を修正しました。性能試験の一時設定にはES Moduleとして読ませる `.mts` を使い、CLI引数を渡せるようNodeの `--` を追加しました。これらはすべて許可された新規試験と `e2e:perf` 内の修正です。既存試験は変更していません。

## 受け入れ判定

| 条件 | 判定 |
|---|---|
| 360/412px縦向きの全ルート、プレビュー両タブ、各操作状態 | ✓ C14 Acceptance |
| 800px横向きのルートと各操作状態 | ✓ C14 Acceptance |
| 915px横向きの全操作状態 | ✗ GitとAI操作を表示したヘッダーが924pxへ拡大 |
| SelectionBarの24px safe-area、ComposerとEditBarのキーボード追従 | ✓ CDPと偽visualViewportによる自動検査 |
| コメントと編集の下書き復元、IME、visibility復帰、結果不明POSTの再送防止 | ✓ C14 Acceptanceと既存回帰 |
| 44pxターゲット、43px検出、コード内スクロール例外、危険操作の8px間隔 | ✓ 360/412/768px |
| 性能計測と記録 | ✓ desktopとmobile-412、各5回 |
| 実機ゲート | ✗ 未実施 |

実機レポートのゲート0確定欄は未記入です。C14では、既存実装およびC9/C11報告で維持されている仮判定を変更していません。

INV-1（画像）、INV-2（計算済みスタイル）、INV-3（マウス操作）、INV-4（改行、BOM、末尾改行、409）、INV-5（1文字入力の再描画行数）は既存desktop試験で確認します。INV-6（API、ルート、localStorageキー）は製品コード差分なしとGo検証、INV-7はGoとVitestで確認します。C14による意図的なdesktop挙動変更はありません。

| デスクトップ不変条件 | 判定と証拠 |
|---|---|
| INV-1 外観 | ✓ 1440/1024/840px、サイドバー400pxを含む既存画像基準 |
| INV-2 計算済みスタイル | ✓ hoverを含む既存JSON基準 |
| INV-3 マウス操作 | ✓ gutter、Shift、ドラッグ、文字範囲、hover、ダブルクリック、編集キー |
| INV-4 保存形式と競合 | ✓ CRLF、BOM、末尾改行、混在改行、409拒否 |
| INV-5 行の再描画 | ✓ 1文字入力の増分が2行以下 |
| INV-6 APIと保存キー | ✓ 製品コード差分なし、Go検証 |
| INV-7 既存単体テスト | ✓ Go成功、Vitest 154成功。実機ログ未収録の1件は既存の条件付きskip |

## C14の最終検証結果

| コマンド | 結果 |
|---|---|
| `npm run typecheck` | ✓ |
| `npm test` | ✓ 10 files、154 passed、1 skipped（実機ログ未収録） |
| `npm run typecheck:e2e` | ✓ |
| `go test ./...` | ✓ 全パッケージ |
| `go vet ./...` | ✓ |
| `npm run build` | ✓ dist差分なし |
| `npm run e2e:desktop` | ✓ 71 passedを3回連続 |
| `npm run e2e:sse` | ✗ 既存スクリプト未定義 |
| `npx playwright test --project=sse` | ✓ 11 passed |
| `npm run e2e:perf` | ✓ 2 passed |
| C14 mobile Acceptanceとtap-targets | 24 passed、1 failed、8 skipped |
| `npm run e2e` | 472 passed、2 failed、24 skipped |
| `git diff --check`、distのスパイク検査 | ✓ エラーなし、対象文字列なし |

全回帰の集計です。

```text
2 failed
24 skipped
472 passed (12.1m)
```

全回帰で失敗したケースです。

```text
✘  179 [mobile-412] › e2e/mobile/acceptance.e2e.ts:119:3 › C14 shell and transient states fit in landscape (2.9s)
✘  486 [tablet-landscape] › e2e/mobile/viewport.e2e.ts:22:1 › touch viewport tracks resize, scroll and rotation with safe-area foundation styles (4.8s)
```

既存viewport試験の今回の結果です。tablet-landscapeの既知flakyが再現しました。回転後にvisualViewportを高さ250px、offsetTopを20pxへ変更した際、`height: "250px"` は一致しましたが、`--kb-inset` と期待値の比較が `matches: false` となり、5秒の待機で失敗しました。`viewport.e2e.ts:52` の既存検査で、テストと製品コードは変更していません。再試行で結果を置き換えていません。

```text
✓  174 [mobile-360] › e2e/mobile/viewport.e2e.ts:22:1 › touch viewport tracks resize, scroll and rotation with safe-area foundation styles (683ms)
✓  175 [mobile-360] › e2e/mobile/viewport.e2e.ts:62:3 › fine pointer viewport › leaves expanded desktop unset and clears variables after returning from narrow (854ms)
✓  278 [mobile-412] › e2e/mobile/viewport.e2e.ts:22:1 › touch viewport tracks resize, scroll and rotation with safe-area foundation styles (815ms)
✓  279 [mobile-412] › e2e/mobile/viewport.e2e.ts:62:3 › fine pointer viewport › leaves expanded desktop unset and clears variables after returning from narrow (937ms)
✓  382 [tablet-768] › e2e/mobile/viewport.e2e.ts:22:1 › touch viewport tracks resize, scroll and rotation with safe-area foundation styles (731ms)
✓  383 [tablet-768] › e2e/mobile/viewport.e2e.ts:62:3 › fine pointer viewport › leaves expanded desktop unset and clears variables after returning from narrow (734ms)
✘  486 [tablet-landscape] › e2e/mobile/viewport.e2e.ts:22:1 › touch viewport tracks resize, scroll and rotation with safe-area foundation styles (4.8s)
✓  487 [tablet-landscape] › e2e/mobile/viewport.e2e.ts:62:3 › fine pointer viewport › leaves expanded desktop unset and clears variables after returning from narrow (690ms)
```

C14専用実行の8件のskipは、`tablet-768`に適用しないスマートフォン用Acceptanceです。全回帰では、同じ適用条件に加え、tablet-landscapeのC14対象外ケースと既存のnarrow専用ケースがskipになります。失敗した横向きケースをskipにはしていません。

検証ログは `/tmp/reviewer-c14-*.log`、C14の最終画面は `/tmp/reviewer-c14-mobile-verified/`、全回帰の失敗情報は `/tmp/reviewer-c14-regression/` にあります。safe-areaを模擬した360pxのSelectionBar、412pxのComposer、ヘッダーシート、915pxのGitダイアログの画像を目視でも確認しました。

## 変更ファイルと引き渡し

`git status --short --untracked-files=all` の出力です。

```text
 M docs/mobile/perf.md
 M web/package.json
?? docs/mobile/known-limitations.md
?? web/e2e/mobile/acceptance.e2e.ts
?? web/e2e/mobile/tap-targets.e2e.ts
?? web/e2e/perf/perf.e2e.ts
```

`git diff --stat` の出力です。未追跡の新規4ファイルはこの統計には含まれません。

```text
 docs/mobile/perf.md | 57 +++++++++++++++++++++++++++++++++++++++++++++++++++++
 web/package.json    |  3 ++-
 2 files changed, 59 insertions(+), 1 deletion(-)
```

受け入れ解消後のコミットメッセージ案は `test: add mobile acceptance and performance suites` です。対象パスは次の6ファイルだけです。distは含めません。

```text
web/e2e/mobile/acceptance.e2e.ts
web/e2e/mobile/tap-targets.e2e.ts
web/e2e/perf/perf.e2e.ts
web/package.json
docs/mobile/perf.md
docs/mobile/known-limitations.md
```

製品コード、既存テスト、基準スナップショット、runtime/devDependencies、package-lockに差分はありません。`package.json` は `e2e:perf` の追加だけです。ステージ、コミット、push、stash、reset、checkout、restore、clean、rebase、merge、デプロイ、systemd操作は行っていません。

判断が必要なのは、C14の対象外にあるexpanded touchヘッダーの修正範囲と実機ゲートです。今回の作業は試験と結果の記録で停止します。次のチェックポイントには進みません。

## コマンド出力

以下に、検証コマンドごとの末尾20行を示します。全文は上記の一時ログにあります。

<details>
<summary>npm run typecheck</summary>

```text

> reviewer-web@0.0.0 typecheck
> tsc --noEmit

```

</details>

<details>
<summary>npm test</summary>

```text

> reviewer-web@0.0.0 test
> vitest run


 RUN  v4.1.11 /home/ymotongpoo/repos/reviewer/web

(node:37) ExperimentalWarning: localStorage is not available because --localstorage-file was not provided.
(Use `node --trace-warnings ...` to show where the warning was created)

 Test Files  10 passed (10)
      Tests  154 passed | 1 skipped (155)
   Start at  07:26:48
   Duration  1.10s (transform 386ms, setup 0ms, import 730ms, tests 330ms, environment 2ms)

```

</details>

<details>
<summary>npm run typecheck:e2e</summary>

```text

> reviewer-web@0.0.0 typecheck:e2e
> tsc -p e2e/tsconfig.json --noEmit

```

</details>

<details>
<summary>go test ./...</summary>

```text
?   	github.com/ymotongpoo/reviewer/cmd/fakehermes	[no test files]
ok  	github.com/ymotongpoo/reviewer/cmd/reviewer	0.006s
?   	github.com/ymotongpoo/reviewer/internal/agent	[no test files]
ok  	github.com/ymotongpoo/reviewer/internal/agent/hermes	0.248s
?   	github.com/ymotongpoo/reviewer/internal/agent/hermes/hermestest	[no test files]
ok  	github.com/ymotongpoo/reviewer/internal/anchor	(cached)
ok  	github.com/ymotongpoo/reviewer/internal/annotate	(cached)
ok  	github.com/ymotongpoo/reviewer/internal/app	1.816s
ok  	github.com/ymotongpoo/reviewer/internal/config	0.005s
ok  	github.com/ymotongpoo/reviewer/internal/feedback	(cached)
ok  	github.com/ymotongpoo/reviewer/internal/gitops	1.060s
ok  	github.com/ymotongpoo/reviewer/internal/project	(cached)
ok  	github.com/ymotongpoo/reviewer/internal/server	0.642s
ok  	github.com/ymotongpoo/reviewer/internal/store	(cached)
ok  	github.com/ymotongpoo/reviewer/internal/textutil	(cached)
```

</details>

<details>
<summary>go vet ./...</summary>

```text
（出力なし、終了コード0）
```

</details>

<details>
<summary>npm run build</summary>

```text
../internal/server/dist/assets/html-CRzt6s9I.js                           57.30 kB │ gzip:  11.78 kB
../internal/server/dist/assets/markdown-BYOwaDjH.js                       59.32 kB │ gzip:   5.66 kB
../internal/server/dist/assets/python-gzcpVVnB.js                         69.94 kB │ gzip:   9.09 kB
../internal/server/dist/assets/c-CKHSTq3t.js                              72.16 kB │ gzip:  10.54 kB
../internal/server/dist/assets/swift-CyEgAFGc.js                          87.22 kB │ gzip:  14.79 kB
../internal/server/dist/assets/latex-ZgXIZN55.js                          89.15 kB │ gzip:  10.96 kB
../internal/server/dist/assets/csharp-oqKa8noW.js                         90.18 kB │ gzip:  10.71 kB
../internal/server/dist/assets/php-C9Bmw1yO.js                           113.08 kB │ gzip:  28.85 kB
../internal/server/dist/assets/asciidoc-SCjQUq34.js                      136.04 kB │ gzip:   9.45 kB
../internal/server/dist/assets/mdx-DQZ5AkYe.js                           136.10 kB │ gzip:  23.54 kB
../internal/server/dist/assets/javascript-BgS3c2Ky.js                    174.82 kB │ gzip:  16.62 kB
../internal/server/dist/assets/tsx-CZ_Wt7l7.js                           175.59 kB │ gzip:  16.67 kB
../internal/server/dist/assets/jsx-B3AKtmIm.js                           177.84 kB │ gzip:  16.76 kB
../internal/server/dist/assets/typescript-DVeVUgnY.js                    181.13 kB │ gzip:  16.28 kB
../internal/server/dist/assets/katex-DnD4SCnU.js                         258.89 kB │ gzip:  77.66 kB
../internal/server/dist/assets/dist-BlSa3Ump.js                          297.38 kB │ gzip: 116.20 kB
../internal/server/dist/assets/index-npy8n0cO.js                         443.97 kB │ gzip: 146.54 kB
../internal/server/dist/assets/cpp-Dm-1-pEP.js                           796.96 kB │ gzip:  60.65 kB

✓ built in 499ms
```

</details>

<details>
<summary>npm run e2e:desktop（1回目）</summary>

```text
  ✓  56 [desktop] › e2e/support/ime.ts:269:3 › native multiline input and paste retain both surrounding lines (615ms)
  ✓  57 [desktop] › e2e/desktop/inputlog.e2e.ts:4:1 › input log is opt-in, persists the flag, exports metadata and can be disabled (1.0s)
  ✓  58 [desktop] › e2e/desktop/interactions.e2e.ts:7:1 › C9 leaves fine-pointer gutters and selection UI unchanged, including narrow windows (793ms)
  ✓  59 [desktop] › e2e/desktop/interactions.e2e.ts:20:1 › gutter click and Shift-click extend a line comment (2.4s)
  ✓  60 [desktop] › e2e/desktop/interactions.e2e.ts:38:1 › gutter drag selects L3 through L6 (1.7s)
  ✓  61 [desktop] › e2e/desktop/interactions.e2e.ts:56:3 › selected text becomes a character range: guide.md (1.7s)
  ✓  62 [desktop] › e2e/desktop/interactions.e2e.ts:56:3 › selected text becomes a character range: emoji.md (1.7s)
  ✓  63 [desktop] › e2e/desktop/interactions.e2e.ts:84:1 › hover controls, double click, Ctrl+S, Escape and i (727ms)
  ✓  64 [desktop] › e2e/desktop/interactions.e2e.ts:108:1 › autosave after debounce, Ctrl+Enter, submit counts and tree navigation (1.7s)
  ✓  65 [desktop] › e2e/desktop/interactions.e2e.ts:126:1 › external file write arrives over SSE (887ms)
  ✓  66 [desktop] › e2e/desktop/styles.e2e.ts:6:1 › computed desktop styles including hovered controls (1.7s)
  ✓  67 [desktop] › e2e/desktop/visual.e2e.ts:12:3 › desktop screens 1440x900 sidebar 280 (5.4s)
  ✓  68 [desktop] › e2e/desktop/visual.e2e.ts:12:3 › desktop screens 1024x768 sidebar 280 (4.2s)
  ✓  69 [desktop] › e2e/desktop/visual.e2e.ts:12:3 › desktop screens 840x900 sidebar 280 (4.0s)
  ✓  70 [desktop] › e2e/desktop/visual.e2e.ts:12:3 › desktop screens 1440x900 sidebar 400 (5.0s)
(node:956) Warning: The 'NO_COLOR' env is ignored due to the 'FORCE_COLOR' env being set.
(Use `node --trace-warnings ...` to show where the warning was created)
  ✓  71 [boundary] › e2e/boundary/layout.e2e.ts:3:1 › 839px drawer returns to the desktop sidebar at 840px with a fine pointer (926ms)

  71 passed (1.6m)
```

</details>

<details>
<summary>npm run e2e:desktop（2回目）</summary>

```text
  ✓  56 [desktop] › e2e/support/ime.ts:269:3 › native multiline input and paste retain both surrounding lines (642ms)
  ✓  57 [desktop] › e2e/desktop/inputlog.e2e.ts:4:1 › input log is opt-in, persists the flag, exports metadata and can be disabled (1.1s)
  ✓  58 [desktop] › e2e/desktop/interactions.e2e.ts:7:1 › C9 leaves fine-pointer gutters and selection UI unchanged, including narrow windows (811ms)
  ✓  59 [desktop] › e2e/desktop/interactions.e2e.ts:20:1 › gutter click and Shift-click extend a line comment (2.4s)
  ✓  60 [desktop] › e2e/desktop/interactions.e2e.ts:38:1 › gutter drag selects L3 through L6 (1.6s)
  ✓  61 [desktop] › e2e/desktop/interactions.e2e.ts:56:3 › selected text becomes a character range: guide.md (1.7s)
  ✓  62 [desktop] › e2e/desktop/interactions.e2e.ts:56:3 › selected text becomes a character range: emoji.md (1.7s)
  ✓  63 [desktop] › e2e/desktop/interactions.e2e.ts:84:1 › hover controls, double click, Ctrl+S, Escape and i (763ms)
  ✓  64 [desktop] › e2e/desktop/interactions.e2e.ts:108:1 › autosave after debounce, Ctrl+Enter, submit counts and tree navigation (1.6s)
  ✓  65 [desktop] › e2e/desktop/interactions.e2e.ts:126:1 › external file write arrives over SSE (859ms)
  ✓  66 [desktop] › e2e/desktop/styles.e2e.ts:6:1 › computed desktop styles including hovered controls (1.7s)
  ✓  67 [desktop] › e2e/desktop/visual.e2e.ts:12:3 › desktop screens 1440x900 sidebar 280 (5.5s)
  ✓  68 [desktop] › e2e/desktop/visual.e2e.ts:12:3 › desktop screens 1024x768 sidebar 280 (4.2s)
  ✓  69 [desktop] › e2e/desktop/visual.e2e.ts:12:3 › desktop screens 840x900 sidebar 280 (4.1s)
  ✓  70 [desktop] › e2e/desktop/visual.e2e.ts:12:3 › desktop screens 1440x900 sidebar 400 (5.1s)
(node:955) Warning: The 'NO_COLOR' env is ignored due to the 'FORCE_COLOR' env being set.
(Use `node --trace-warnings ...` to show where the warning was created)
  ✓  71 [boundary] › e2e/boundary/layout.e2e.ts:3:1 › 839px drawer returns to the desktop sidebar at 840px with a fine pointer (970ms)

  71 passed (1.6m)
```

</details>

<details>
<summary>npm run e2e:desktop（3回目）</summary>

```text
  ✓  56 [desktop] › e2e/support/ime.ts:269:3 › native multiline input and paste retain both surrounding lines (647ms)
  ✓  57 [desktop] › e2e/desktop/inputlog.e2e.ts:4:1 › input log is opt-in, persists the flag, exports metadata and can be disabled (1.0s)
  ✓  58 [desktop] › e2e/desktop/interactions.e2e.ts:7:1 › C9 leaves fine-pointer gutters and selection UI unchanged, including narrow windows (848ms)
  ✓  59 [desktop] › e2e/desktop/interactions.e2e.ts:20:1 › gutter click and Shift-click extend a line comment (2.4s)
  ✓  60 [desktop] › e2e/desktop/interactions.e2e.ts:38:1 › gutter drag selects L3 through L6 (1.6s)
  ✓  61 [desktop] › e2e/desktop/interactions.e2e.ts:56:3 › selected text becomes a character range: guide.md (1.7s)
  ✓  62 [desktop] › e2e/desktop/interactions.e2e.ts:56:3 › selected text becomes a character range: emoji.md (1.7s)
  ✓  63 [desktop] › e2e/desktop/interactions.e2e.ts:84:1 › hover controls, double click, Ctrl+S, Escape and i (737ms)
  ✓  64 [desktop] › e2e/desktop/interactions.e2e.ts:108:1 › autosave after debounce, Ctrl+Enter, submit counts and tree navigation (1.7s)
  ✓  65 [desktop] › e2e/desktop/interactions.e2e.ts:126:1 › external file write arrives over SSE (900ms)
  ✓  66 [desktop] › e2e/desktop/styles.e2e.ts:6:1 › computed desktop styles including hovered controls (1.6s)
  ✓  67 [desktop] › e2e/desktop/visual.e2e.ts:12:3 › desktop screens 1440x900 sidebar 280 (5.3s)
  ✓  68 [desktop] › e2e/desktop/visual.e2e.ts:12:3 › desktop screens 1024x768 sidebar 280 (4.2s)
  ✓  69 [desktop] › e2e/desktop/visual.e2e.ts:12:3 › desktop screens 840x900 sidebar 280 (4.1s)
  ✓  70 [desktop] › e2e/desktop/visual.e2e.ts:12:3 › desktop screens 1440x900 sidebar 400 (5.0s)
(node:1981) Warning: The 'NO_COLOR' env is ignored due to the 'FORCE_COLOR' env being set.
(Use `node --trace-warnings ...` to show where the warning was created)
  ✓  71 [boundary] › e2e/boundary/layout.e2e.ts:3:1 › 839px drawer returns to the desktop sidebar at 840px with a fine pointer (925ms)

  71 passed (1.6m)
```

</details>

<details>
<summary>npm run e2e:sse</summary>

```text
npm error Missing script: "e2e:sse"
npm error
npm error To see a list of scripts, run:
npm error   npm run
npm error A complete log of this run can be found in: /home/ymotongpoo/.npm/_logs/2026-10-04T07_31_23_157Z-debug-0.log
```

</details>

<details>
<summary>npx playwright test --project=sse</summary>

```text

Running 11 tests using 1 worker

(node:2129) Warning: The 'NO_COLOR' env is ignored due to the 'FORCE_COLOR' env being set.
(Use `node --trace-warnings ...` to show where the warning was created)
  ✓   1 [sse] › e2e/sse/resync.e2e.ts:59:1 › restart resyncs a file changed while the server was stopped (8.9s)
  ✓   2 [sse] › e2e/sse/resync.e2e.ts:69:1 › a delayed old snapshot cannot overwrite the newer sync or buffered comments (5.6s)
  ✓   3 [sse] › e2e/sse/resync.e2e.ts:97:1 › visible return refetches project state and retains the one live EventSource (3.9s)
  ✓   4 [sse] › e2e/sse/resync.e2e.ts:111:1 › resync preserves the edit base and draft, so saving against external changes returns 409 (13.4s)
  ✓   5 [sse] › e2e/sse/resync.e2e.ts:129:1 › SSE comments and a visibility sync preserve the unsaved Composer body (5.1s)
  ✓   6 [sse] › e2e/sse/resync.e2e.ts:150:1 › close preserves the existing API behavior: the next GET reopens the project (3.9s)
  ✓   7 [sse] › e2e/sse/resync.e2e.ts:160:1 › a forgotten project returns 404 on resync and shows unavailable with a picker link (959ms)
  ✓   8 [sse] › e2e/sse/resync.e2e.ts:170:1 › restart with a different token yields unauthorized without discarding the page (6.2s)
  ✓   9 [sse] › e2e/sse/resync.e2e.ts:177:1 › resync restores a missed response banner and dismissal persists across reload (9.4s)
  ✓  10 [sse] › e2e/sse/resync.e2e.ts:200:1 › malformed SSE warns without an uncaught exception and a subsequent stream syncs (7.7s)
  ✓  11 [sse] › e2e/sse/resync.e2e.ts:217:1 › terminal errors before the first sync display the same guidance (886ms)

  11 passed (1.1m)
```

</details>

<details>
<summary>npm run e2e:perf</summary>

```text
Running 2 tests using 1 worker

(node:88) Warning: The 'NO_COLOR' env is ignored due to the 'FORCE_COLOR' env being set.
(Use `node --trace-warnings ...` to show where the warning was created)
C14 sample desktop 1/5: render=748.3ms, scroll-longtask=0ms
C14 sample desktop 2/5: render=755.2ms, scroll-longtask=0ms
C14 sample desktop 3/5: render=761.1ms, scroll-longtask=0ms
C14 sample desktop 4/5: render=734.0ms, scroll-longtask=0ms
C14 sample desktop 5/5: render=811.4ms, scroll-longtask=0ms
C14_PERF {"device":"desktop","browser":"153.0.8010.12","summary":{"renderMedianMs":755.2000000178814,"maxLongtaskMedianMs":0,"maxLongtaskMs":0,"inputFrameMedianMs":67.19999998807907,"inputFrameMaxMs":292.30000001192093,"eventDelayMaxMs":64.89999997615814},"path":"/tmp/reviewer-c14-perf-final/perf.e2e.ts-C14-performanc-832a2-00-rows-CPU-4x-five-samples-perf/perf-desktop.json"}
  ✓  1 [perf] › e2e/perf/perf.e2e.ts:8:3 › C14 performance: desktop, 3000 rows, CPU 4x, five samples (1.9m)
C14 sample mobile-412 1/5: render=1083.9ms, scroll-longtask=0ms
C14 sample mobile-412 2/5: render=1040.5ms, scroll-longtask=0ms
C14 sample mobile-412 3/5: render=1024.7ms, scroll-longtask=0ms
C14 sample mobile-412 4/5: render=995.6ms, scroll-longtask=0ms
C14 sample mobile-412 5/5: render=968.0ms, scroll-longtask=0ms
C14_PERF {"device":"mobile-412","browser":"153.0.8010.12","summary":{"renderMedianMs":1024.7000000178814,"maxLongtaskMedianMs":0,"maxLongtaskMs":0,"inputFrameMedianMs":81.5,"inputFrameMaxMs":338.2000000178814,"eventDelayMaxMs":89.09999999403954},"path":"/tmp/reviewer-c14-perf-final/perf.e2e.ts-C14-performanc-3b4d3-00-rows-CPU-4x-five-samples-perf/perf-mobile-412.json"}
  ✓  2 [perf] › e2e/perf/perf.e2e.ts:8:3 › C14 performance: mobile-412, 3000 rows, CPU 4x, five samples (2.3m)

  2 passed (4.3m)
```

</details>

<details>
<summary>npx playwright test --project=mobile-360 --project=mobile-412 --project=tablet-768 'mobile/(acceptance|tap-targets)\.e2e\.ts'</summary>

```text
    ────────────────────────────────────────────────────────────────────────────────────────────────

    attachment #9: screenshot (image/png) ──────────────────────────────────────────────────────────
    ../../../../../tmp/reviewer-c14-mobile-verified/mobile-acceptance.e2e.ts-C-a7039-ent-states-fit-in-landscape-mobile-412/test-failed-1.png
    ────────────────────────────────────────────────────────────────────────────────────────────────

    Error Context: ../../../../../tmp/reviewer-c14-mobile-verified/mobile-acceptance.e2e.ts-C-a7039-ent-states-fit-in-landscape-mobile-412/error-context.md

    attachment #11: trace (application/zip) ────────────────────────────────────────────────────────
    ../../../../../tmp/reviewer-c14-mobile-verified/mobile-acceptance.e2e.ts-C-a7039-ent-states-fit-in-landscape-mobile-412/trace.zip
    Usage:

        npx playwright show-trace ../../../../../tmp/reviewer-c14-mobile-verified/mobile-acceptance.e2e.ts-C-a7039-ent-states-fit-in-landscape-mobile-412/trace.zip

    ────────────────────────────────────────────────────────────────────────────────────────────────

  1 failed
    [mobile-412] › e2e/mobile/acceptance.e2e.ts:119:3 › C14 shell and transient states fit in landscape
  8 skipped
  24 passed (1.0m)
```

</details>

<details>
<summary>npm run e2e</summary>

```text

    attachment #1: screenshot (image/png) ──────────────────────────────────────────────────────────
    ../../../../../tmp/reviewer-c14-regression/mobile-viewport.e2e.ts-tou-13162-safe-area-foundation-styles-tablet-landscape/test-failed-1.png
    ────────────────────────────────────────────────────────────────────────────────────────────────

    Error Context: ../../../../../tmp/reviewer-c14-regression/mobile-viewport.e2e.ts-tou-13162-safe-area-foundation-styles-tablet-landscape/error-context.md

    attachment #3: trace (application/zip) ─────────────────────────────────────────────────────────
    ../../../../../tmp/reviewer-c14-regression/mobile-viewport.e2e.ts-tou-13162-safe-area-foundation-styles-tablet-landscape/trace.zip
    Usage:

        npx playwright show-trace ../../../../../tmp/reviewer-c14-regression/mobile-viewport.e2e.ts-tou-13162-safe-area-foundation-styles-tablet-landscape/trace.zip

    ────────────────────────────────────────────────────────────────────────────────────────────────

  2 failed
    [mobile-412] › e2e/mobile/acceptance.e2e.ts:119:3 › C14 shell and transient states fit in landscape
    [tablet-landscape] › e2e/mobile/viewport.e2e.ts:22:1 › touch viewport tracks resize, scroll and rotation with safe-area foundation styles
  24 skipped
  472 passed (12.1m)
```

</details>
