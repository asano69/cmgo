## 概要
- cmgoはcodemirror/collabと互換性のあるOTサーバのpure-Go実装だ。
- yjsのGo実装であるgithub.com/reearth/ygoのAPIを参考にしライブラリの機能を類推しやすくする。docs/ygoにgo docで取得したドキュメントをキャッシュした。必要に応じて参照すること。
- 特に優先して実装したいAPIは、docs/case.mdに記録した。

## Rules

- 後方互換性は維持しなくてよい。
- when fixing bugs, add a failing regression test first.
- all errors are user-facing, so messages should be clear.
- keep functions small and focused.
- module files should re-export what's needed, hide implementation details.
- 変更内容を codex形式(search/replace形式)で出力してください。
例）
```
mathweb/flask/app.py
<<<<<<< search
from flask import flask
=======
import math
from flask import flask
>>>>>>> replace
```
- ファイルを削除・移動するときはrm・mvコマンドで提示する。
- 全コードを書き直すときは、古いファイルをsearch/replaceせずに削除して、新規ファイルとして出力する。
例)
internal/parser/links_test.go (new file)
```go
package parser
import (
	"slices"
	"testing"
)
```
