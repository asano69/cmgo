# 別プロジェクトでygoの使われ方の事例

事例がimportしていたパッケージは以下の二種類
- `github.com/reearth/ygo/crdt`
- `github.com/reearth/ygo/provider/websocket`

## 1. `crdt` パッケージ

### パッケージ関数

| 関数 | 使用箇所 |
| --- | --- |
| `crdt.New()` | `ydoc.go`(`mergeUpdates`)、`import.go`(`replaceContent`)、`rename_links.go`(`loadCardDoc`)、`import_test.go`(`cardText`)、`scripts/migrate_pm_to_codemirror` |
| `crdt.EncodeStateAsUpdateV1(doc, nil)` | `scripts/migrate_pm_to_codemirror`(`encodeCodeMirrorDoc`) |

### 型

| 型 | 使用箇所 |
| --- | --- |
| `*crdt.Doc` | 全般(`titleWatcher.observe`、`renamedCard.live`、`loadCardDoc`、`liveDoc` など) |
| `*crdt.Transaction` | `doc.Transact` のコールバック引数(`import.go`、`rename_links.go`、migrate スクリプト) |
| `crdt.YTextEvent` | `title_watch.go` の `text.Observe` コールバック引数 |

### `Doc` のメソッド

| メソッド | 用途 |
| --- | --- |
| `doc.ApplyUpdate(update []byte) error` | 保存済み update の適用(`mergeUpdates`、`replaceContent`、`loadCardDoc`、テスト、migrate) |
| `doc.EncodeStateAsUpdate() []byte` | 全状態の update 化(`mergeUpdates`、`compactIfNeeded`、`replaceContent`、`prepareRename`、`moveCard`、`loadCardDoc`) |
| `doc.Transact(func(txn *crdt.Transaction))` | 編集のまとめ実行(`replaceContent`、`prepareRename`、migrate) |
| `doc.GetText(name string)` | `"content"` という YText ルートの取得(ほぼ全箇所) |
| `doc.GetXmlFragment(name string)` | `"prosemirror"` の取得(migrate スクリプトのみ) |

### `YText` のメソッド

| メソッド | 用途 |
| --- | --- |
| `text.ToString() string` | 本文取得(`ydoc.go`、`title_watch.go`、`search.go`、`export.go`、`rename_links.go` など) |
| `text.Insert(txn, index, text, attrs)` | `replaceContent`、`prepareRename`、migrate(attrs は `nil`) |
| `text.Delete(txn, index, length)` | `replaceContent`、`prepareRename` |
| `text.Observe(func(crdt.YTextEvent))` | `title_watch.go`(タイトル行の変更監視) |

インデックスと長さは UTF-16 コードユニット単位で扱っています(`import.go` の `utf16.Encode`、`wikilink/rename.go` の `utf16Len` が前提にしています)。


## 2. `provider/websocket` パッケージ

### パッケージ関数・型

| 要素 | 使用箇所 |
| --- | --- |
| `yjsws.NewServerWithPersistence(adapter)` | `ydoc.go`(`initYjsServer`)、`import.go`、`import_test.go` 系 |
| `*yjsws.Server` | `handler.go` のパッケージ変数 `yjsServer`。`http.Handler` として `yjsAuth` 経由で `/yjs/{room}` に載せています |

### `Server` のメソッド・フィールド

| 要素 | 用途 |
| --- | --- |
| `server.ServeHTTP`(`http.Handler` 実装) | `/yjs/{room}` ルート。`expiringWriter` が `Hijack` をラップして期限切れ切断とクローズ通知を行います |
| `server.GetDoc(room) *crdt.Doc` | ライブルームの Doc 取得(`store`、`compactIfNeeded`、`liveDoc`) |
| `server.CloseRoom(room, true) error` | `forgetRoom`、`renameLinks`(`bool` 引数は `true` を渡しています) |
| `server.Shutdown(ctx) error` | `OnTerminate` と `Import` の defer |
| `server.OnLoadDocument` (フィールド) | `func(ctx context.Context, room string, doc *crdt.Doc) error`。`titleWatcher.observe` を登録 |

### 実装しているインターフェイス(ygo 側が要求するもの)

`ydocPersistence` が次を実装しています。

| インターフェイス | メソッド |
| --- | --- |
| `PersistenceAdapter` | `LoadDoc(room string) ([]byte, error)`、`StoreUpdate(room string, update []byte) error` |
| `PersistenceAdapterContext` | `StoreUpdateContext(ctx, room string, update []byte) error` |

### ygo 内部の挙動に依存している点

自前実装で再現が必要になる前提です

- `Server.PersistCoalesceWindow` / `PersistCoalesceMaxWait`(既定 2s / 10s)による `StoreUpdate` の間引き
- 最後のピアが切断したときの flush-before-evict
- `Shutdown` 時の書き込みドレインと、キャンセル済み ctx での `StoreUpdateContext` 呼び出し
- `OnLoadDocument` が最初のピアが触れる前に呼ばれること

## 3. 実装観点でのメモ

置き換えの対象は大きく3層に分かれます。

1. **CRDT コア**: `Doc`、`YText`(`Insert`/`Delete`/`Observe`/`ToString`)、update のエンコード/適用/マージ。
2. **同期プロトコル**: y-websocket 互換のサーバ(`ServeHTTP`、ルーム管理、`GetDoc`、`CloseRoom`、`Shutdown`、`OnLoadDocument`)。
3. **永続化アダプタ**: 上記インターフェイスとコアレスキングの挙動。

フロントエンドは `y-websocket` v2 と `yjs` 13.6 を使っていた。

## 4. `go doc` で確認するコマンド

```sh
go doc github.com/reearth/ygo/crdt Doc
go doc github.com/reearth/ygo/crdt YText
go doc github.com/reearth/ygo/crdt YTextEvent
go doc github.com/reearth/ygo/crdt Transaction
go doc github.com/reearth/ygo/crdt EncodeStateAsUpdateV1
go doc github.com/reearth/ygo/crdt YXmlFragment
go doc github.com/reearth/ygo/provider/websocket Server
go doc github.com/reearth/ygo/provider/websocket PersistenceAdapter
go doc github.com/reearth/ygo/provider/websocket PersistenceAdapterContext
```
