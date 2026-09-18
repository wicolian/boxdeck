# Boxdeck UX audit

## Findings

[Hick] desktop rail/default - sixteen ungrouped destinations force the user to scan the full rail before choosing a surface - evidence (captures/ux/before/overview-desktop.png)
[Miller] desktop rail/default - Work, fleet, monitoring, and settings links read as one long list instead of memorable groups - evidence (captures/ux/before/overview-desktop.png)
[Proximity] desktop rail/default - Herd sits apart from Agents even though both expose agent sessions and controls - evidence (captures/ux/before/agents-desktop.png, captures/ux/before/herd-desktop.png)
[Jakob] desktop rail/default - the rail changes order as modules register, so Apps, Alerts, Herd, Browser, and Network do not follow a stable information architecture - evidence (captures/ux/before/apps-desktop.png, captures/ux/before/network-desktop.png)
[Fitts] phone navigation/default - the More action is the only path to most views and does not communicate the grouped destination count - evidence (captures/ux/before/overview-phone.png)
[Miller] phone More sheet/default - the ungrouped sheet repeats the full rail without sections, increasing search time on a small screen - evidence (captures/ux/before/overview-phone.png)
[Serial position] phone navigation/default - the persistent bar reserves equal space for Ports and Terminal while Alerts and the current need state are not promoted - evidence (captures/ux/before/overview-phone.png)
[Hick] desktop overview/default - quota pills, live status, quick links, health meters, and work lists compete before the first actionable work item - evidence (captures/ux/before/overview-desktop.png)
[Pareto] desktop overview/default - the page makes the user read through four health cells before seeing agents that may need attention - evidence (captures/ux/before/overview-desktop.png)
[Proximity] desktop overview/default - Files and Terminal quick links are separated from the Berths and Apps lists they duplicate - evidence (captures/ux/before/overview-desktop.png)
[Pragnanz] desktop overview/default - the large live health band repeats information that is not needed to open a berth or focus an agent - evidence (captures/ux/before/overview-desktop.png)
[Fitts] desktop overview/default - the tiny All ports and All agents links are farther from the rows that they expand than the row content is from the pointer - evidence (captures/ux/before/overview-desktop.png)
[Jakob] desktop view headings/default - Browser, Files, Git, Docker, and Boxes show an outer masthead title plus an inner outlined title, which reads like two page titles - evidence (captures/ux/before/browser-desktop.png, captures/ux/before/files-desktop.png, captures/ux/before/git-desktop.png, captures/ux/before/docker-desktop.png, captures/ux/before/boxes-desktop.png)
[Pragnanz] desktop view headings/default - the outlined focus treatment on several titles looks like a leftover keyboard state rather than intentional location context - evidence (captures/ux/before/apps-desktop.png, captures/ux/before/browser-desktop.png)
[Similarity] desktop controls/default - core controls, Network controls, Apps controls, and Alerts tabs use visibly different heights, padding, and emphasis for the same actions - evidence (captures/ux/before/apps-desktop.png, captures/ux/before/network-desktop.png, captures/ux/before/alerts-desktop.png)
[Similarity] phone controls/default - primary actions vary between 44px, 52px, and 56px targets, so touch confidence changes from view to view - evidence (captures/ux/before/apps-phone.png, captures/ux/before/browser-phone.png, captures/ux/before/alerts-phone.png)
[Fitts] phone filters/default - list filters are full width but sort controls move to a second row with a small summary squeezed between labels - evidence (captures/ux/before/ports-phone.png, captures/ux/before/processes-phone.png)
[Doherty] desktop data load/default - the large health band and process table initially need a visible waiting state but only show terse placeholder text and can shift the layout when data lands - evidence (captures/ux/before/processes-desktop.png)
[Doherty] phone data load/default - Apps, Boxes, Network, and Agents reserve different amounts of space while fetches complete, making the page jump under the thumb - evidence (captures/ux/before/apps-phone.png, captures/ux/before/boxes-phone.png, captures/ux/before/network-phone.png)
[Empty state] desktop Docker/empty - the message says to start Docker and a container but gives no direct next action or explanation of whether Docker is installed - evidence (captures/ux/before/docker-desktop.png)
[Empty state] desktop Git/empty - no repositories found is presented without the configured roots or a way to change or inspect them - evidence (captures/ux/before/git-desktop.png)
[Empty state] desktop Files/empty editor - No file open gives no instruction in the editor surface while the tree is the actual next step - evidence (captures/ux/before/files-desktop.png)
[Empty search] desktop Overview/filter - a nonmatching filter leaves many section headers and separate Clear the filter buttons instead of one consistent result message - evidence (captures/ux/before/empty-filter-desktop.png)
[Empty search] phone Overview/filter - the empty filter state keeps unrelated Apps content visible, so the user cannot tell whether the filter is global or section-specific - evidence (captures/ux/before/empty-filter-desktop.png)
[Empty state] phone Alerts/quiet - the quiet message explains the rule categories but does not tell the user whether quiet means no alerts, disabled rules, or filtered results - evidence (captures/ux/before/alerts-phone.png)
[Empty state] desktop Browser/stopped - the stopped browser area repeats the start instruction in the large blank canvas and again below it - evidence (captures/ux/before/browser-desktop.png)
[Empty state] phone Browser/stopped - the blank canvas pushes the URL controls below the fold before the user has started a browser - evidence (captures/ux/before/browser-phone.png)
[Proximity] desktop Apps/default - cards reserve similar vertical height for running, detected, and missing apps even when their actions and metadata differ substantially - evidence (captures/ux/before/apps-desktop.png)
[Pragnanz] phone Apps/default - every app card repeats a divider, readiness line, and two large actions, making the catalog feel taller than the decisions require - evidence (captures/ux/before/apps-phone.png)
[Von Restorff] desktop Apps/default - Open, Start, Install hint, and More compete as equally bordered controls even though only one is the primary next action - evidence (captures/ux/before/apps-desktop.png)
[Jakob] phone Apps/default - More is implemented as a disclosure menu while other views use direct secondary buttons, so action discovery differs by surface - evidence (captures/ux/before/apps-phone.png)
[Hick] desktop Alerts/default - five tabs plus two filters plus a snooze action are presented before the alert list, with no grouping between inbox state and configuration - evidence (captures/ux/before/alerts-desktop.png)
[Peak-End] desktop Alerts/quiet - an all-clear inbox ends in a neutral bordered block rather than a clear success state that reassures the user the system is working - evidence (captures/ux/before/alerts-desktop.png)
[Fitts] phone Alerts/default - Delivery wraps to a second tab row and Snooze all becomes an isolated third row, increasing thumb travel for a quiet inbox - evidence (captures/ux/before/alerts-phone.png)
[Proximity] desktop Network/default - Tailscale identity, local interfaces, mirrors, and discovered devices are split into cards without a summary of the device count or online state - evidence (captures/ux/before/network-desktop.png)
[Miller] phone Network/default - the peer list is a long undifferentiated stream of names and addresses with no online, offline, or Boxdeck grouping - evidence (captures/ux/before/network-phone.png)
[Honesty] desktop Network/default - an install command is repeated inside peer rows without making clear whether the peer is reachable, a compatible machine, or a phone - evidence (captures/ux/before/network-desktop.png)
[Proximity] desktop Boxes/default - a single box card leaves the rest of the page empty and does not distinguish configured decks from all tailnet devices - evidence (captures/ux/before/boxes-desktop.png)
[Serial position] phone Boxes/default - the card's Open deck action is below the first viewport and the filter count is far from the device card - evidence (captures/ux/before/boxes-phone.png)
[Pragnanz] desktop Settings/default - raw config keys and large formatted JSON are exposed as a long diagnostic wall without categories or a first action - evidence (captures/ux/before/settings-desktop.png)
[Legibility] phone Settings/default - raw JSON values create a tall scroll of low-priority detail before any setting can be understood or changed - evidence (captures/ux/before/settings-phone.png)
[Fitts] desktop Processes/stop confirmation - the confirmation replaces Stop with a text-heavy action pair inside a wide table row, and the safe cancel affordance is only an unlabeled symbol - evidence (captures/ux/before/process-stop-desktop.png)
[Error recovery] desktop Processes/filter - an empty filtered table relies on the shared message but does not preserve the total count near the user's query - evidence (captures/ux/before/empty-filter-desktop.png)
[Keyboard] desktop help sheet/default - the shortcut sheet lists only five destinations even though eleven other views exist, so the advertised keyboard model is incomplete - evidence (captures/ux/before/help-desktop.png)
[Keyboard] phone help sheet/default - the sheet is not shown in the phone baseline and the More navigation does not expose keyboard help as a visible action - evidence (captures/ux/before/overview-phone.png)
[Responsive] phone terminal/default - the embedded terminal has its own dense tabs and status bars inside a viewport already constrained by the bottom navigation, reducing usable command space - evidence (captures/ux/before/terminal-phone.png)
[Responsive] phone Git/default - the repository selector and repository summary are both truncated, making the active repository hard to verify before a Git action - evidence (captures/ux/before/git-phone.png)

## Good and protected

- The dark bridge palette is restrained, with one amber accent that communicates live or attention states. Protect the palette and its semantic meaning.
- The live health cells make CPU, memory, network, and disk movement scannable at a glance. Keep the compact charts and tabular numerals.
- The phone bottom bar already keeps Overview, Ports, Agents, Terminal, and More reachable with large targets. Preserve the five-slot model while improving the labels and More grouping.
- Shared filters use real search inputs and preserve the user's query while data refreshes. Keep the persistent filter state and improve the empty copy around it.
- Inline process confirmation avoids a modal context switch and already supports Escape through the global handler. Keep the inline pattern and make its actions clearer.
- Files has a useful tree and editor split on desktop and explicit Tree and Editor modes on mobile. Preserve the two-mode model.
- Apps sorts running tools ahead of detected and missing tools, which matches the user's likely next action. Keep the status ordering and make the cards denser.
- Network exposes copy buttons for addresses and reports online state. Keep copy feedback and make device grouping more explicit.

## Needs backend

- Add an optional `lastSeen` field to `/api/net/peers`. The current peer payload has name, OS, online state, and IPs but cannot show the requested last-seen time for offline devices. The UI will render `last seen unavailable` until it exists.
