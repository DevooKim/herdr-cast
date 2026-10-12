# herdr-cast

[English](README.md) | **한국어**

![herdr-cast 팔레트](docs/screenshot.png)

[Herdr](https://herdr.dev)용 Spotlight/Raycast 스타일 커맨드 팔레트입니다. 단축키 하나로 팝업을 열어 검색하고 Enter로 실행합니다.

| 범주 | 조회 | 실행 |
| --- | --- | --- |
| Agents | 모든 에이전트. 상태 순(blocked → done → working → idle)으로 정렬 | 해당 pane으로 이동 (`agent.focus`) |
| Workspaces | 사이드바 순서, 탭/pane 수, 에이전트 상태 | 워크스페이스로 이동 |
| Tabs | 워크스페이스별 탭바 순서 | 탭으로 이동 |
| Plugin actions | 활성화된 플러그인의 모든 `[[actions]]` | 액션 실행. 바인딩된 키가 있으면 같이 표시 |
| Commands | `config.toml`의 `[[keys.command]]` | `shell` / `pane` / `popup` / `plugin_action` 타입별로 Herdr와 같은 방식으로 실행 |
| Keybindings | Herdr 기본값 + 사용자 설정을 합친 실제 키맵 | 대응하는 소켓 API로 실행 (아래 참고) |

검색은 fzf식 퍼지 매칭이고 한글 초성 검색(`ㅇㄷ` → `인덱스 DB`)을 지원합니다. 공백으로 나눈 단어는 순서와 관계없이 모두 맞아야 합니다(`tab new` → `New tab`).

## 설치

Go 1.24 이상이 필요합니다.

```bash
# GitHub에서 설치 (설치 시 go build 실행)
herdr plugin install DevooKim/herdr-cast

# 또는 로컬 체크아웃을 링크
go build -o bin/herdr-cast ./cmd/herdr-cast
herdr plugin link .
```

`~/.config/herdr/config.toml`에 단축키를 추가하고 `herdr server reload-config`(또는 `prefix+shift+r`)로 반영합니다.

```toml
[[keys.command]]
key = "prefix+space"
type = "plugin_action"
command = "devookim.herdr-cast.open"
description = "command palette"
```

## 설정

플러그인 설정 폴더(`herdr plugin config-dir devookim.herdr-cast`, 보통 `~/.config/herdr/plugins/config/devookim.herdr-cast/`)에 `config.toml`을 두면 됩니다. 파일이 없으면 기본값으로 동작하고, 변경 내용은 다음에 팔레트를 열 때 반영됩니다.

```toml
# 쓰지 않을 범주를 숨깁니다. 목록, 상단 범주 바, tab 전환에서 모두 빠집니다.
# agents, workspaces, tabs, plugin_actions, commands, keybindings
disabled_scopes = ["keybindings", "commands"]

# 팔레트 팝업 크기. 퍼센트 문자열 또는 터미널 칸 수. 생략하면 80% × 75%.
width = "80%"
height = "75%"
```

알 수 없는 범주 이름이나 잘못된 크기는 팔레트 하단에 경고로 표시되고, 크기는 기본값으로 돌아갑니다.

## 조작

| 키 | 동작 |
| --- | --- |
| 입력 | 검색 |
| `↑` `↓` / `ctrl+p` `ctrl+n` / `ctrl+k` `ctrl+j` | 이동 |
| `pgup` `pgdown` `home` `end` | 페이지/처음/끝 |
| `←` `→` | 이전/다음 범주의 첫 항목으로 이동 (검색창 커서는 `ctrl+b` `ctrl+f`) |
| `enter` | 실행 |
| `tab` / `shift+tab` | 범주 전환 (All → Agents → Workspaces → Tabs → Plugin actions → Commands → Keybindings). 마지막 범주는 다음에 열 때도 유지 |
| `esc` | 검색어 지우기, 비어 있으면 닫기 |
| `ctrl+c` | 닫기 |

이름이 필요한 항목(new tab, rename workspace 등)은 팔레트 안에서 입력창을 띄우고, 닫기 동작(close pane/tab/workspace)은 확인을 받습니다.

## 키 바인딩 실행 범위

플러그인이 Herdr 내장 키 동작을 직접 호출하는 API는 없습니다. 그래서 Herdr 클라이언트가 키를 처리하는 방식(`src/client/shell/actions.rs`)을 따라 각 동작을 소켓 API 호출로 옮겼습니다.

- **실행 가능**: reload config, new/rename/close/previous/next workspace, new worktree, previous/next agent, new/rename/close/previous/next tab, move tab left/right, split, close/rename/clear pane, edit scrollback, zoom, focus/swap/resize pane, cycle pane
- **키 안내만**: help, settings, detach, workspace navigation, session navigator, open/remove worktree, open notification target, copy mode, resize mode, toggle sidebar, last pane, `1..9` 인덱스 이동. 클라이언트 UI 안에서만 존재하는 동작이라 플러그인에서는 실행할 수 없습니다. 선택하면 눌러야 할 키를 알려줍니다.

키 이름이 없는 동작(예: unbound인 `next_agent`)도 팔레트에서는 실행할 수 있습니다.

Clear pane은 `pane.clear`가 추가된 Herdr 0.9.2 이상 서버에서만 목록에 나옵니다.

## 동작 방식

- 팔레트는 `placement = "popup"` 플러그인 pane입니다. 팝업에는 pane ID가 없으므로 대상 위치는 `HERDR_PLUGIN_CONTEXT_JSON`(팝업 아래 타일 pane)에서 읽습니다.
- 데이터는 `session.snapshot`, `agent.list`, `plugin.list`를 병렬로 조회하고, UI는 그 응답을 기다리지 않고 바로 뜹니다.
- 키맵은 내장 기본값 → `herdr --default-config` → 사용자 `config.toml` 순으로 덮어씁니다.
- Herdr는 팝업을 한 번에 하나만 허용합니다(`ui_busy`). 그래서 팝업·오버레이를 여는 작업(플러그인 액션, `pane`/`popup` 커맨드, edit scrollback)은 detached `herdr-cast exec` 프로세스로 넘깁니다. 이 프로세스는 팔레트가 종료되고 팝업이 닫힌 뒤에 실행하며, 실패하면 Herdr 알림으로 알려줍니다. 포커스 이동처럼 바로 끝나는 API 호출은 팔레트 안에서 실행하고, 오류는 하단에 표시합니다.
- `pane`/`popup` 커스텀 커맨드는 Herdr처럼 `/bin/sh -c`로, `shell` 커맨드는 `/bin/sh -lc`로 포커스된 pane의 cwd에서 실행됩니다. `HERDR_ACTIVE_WORKSPACE_ID`, `HERDR_ACTIVE_TAB_ID`, `HERDR_ACTIVE_PANE_ID`, `HERDR_ACTIVE_PANE_CWD`도 똑같이 넘깁니다.

## 개발

```bash
go test ./...
go build -o bin/herdr-cast ./cmd/herdr-cast

# UI 없이 현재 세션 기준으로 항목 확인 (Herdr pane 안에서 실행)
./bin/herdr-cast list            # 전체
./bin/herdr-cast list split      # 검색

# 팝업 열기
herdr plugin action invoke devookim.herdr-cast.open
```

지연 실행 작업의 오류는 `$HERDR_PLUGIN_STATE_DIR/exec.log`에도 남습니다.
