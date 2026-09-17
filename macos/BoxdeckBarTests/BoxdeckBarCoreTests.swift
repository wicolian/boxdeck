import Foundation
import XCTest
@testable import BoxdeckBar

final class BoxdeckBarCoreTests: XCTestCase {
    func testDecodesGoBarFixtures() throws {
        let state = try decode(BoxState.self, named: "state")
        XCTAssertEqual(state.health.cpu, 12)
        XCTAssertEqual(state.agents.count, 3)
        XCTAssertEqual(state.ports.count, 5)
        XCTAssertEqual(state.agents[1].status, "needs_you")

        let usage = try decode(UsageResponse.self, named: "usage")
        XCTAssertEqual(usage.providers["claude"]?.quota?.fiveHour?.pct, 20)
        XCTAssertEqual(usage.providers["claude"]?.today.tokens.total, 515654016)

        let usageAll = try decode(UsageAllResponse.self, named: "usage-all")
        XCTAssertEqual(usageAll.boxes.count, 3)
        XCTAssertTrue(usageAll.boxes[2].discovered)

        let boxes = try decode([BoxCardResponse].self, named: "boxes")
        XCTAssertEqual(boxes.count, 2)
        XCTAssertEqual(boxes[1].agents, 4)

        let peers = try decode([Peer].self, named: "peers")
        XCTAssertEqual(peers.count, 2)
        XCTAssertFalse(peers[1].boxdeck)
    }

    func testBuildsMenuTextFromFixture() throws {
        let state = try decode(BoxState.self, named: "state")
        let usage = try decode(UsageResponse.self, named: "usage")
        let box = BoxSnapshot(
            name: "box",
            url: "http://box:8100",
            ok: true,
            state: state,
            usage: usage
        )

        let model = MenuModelBuilder.build(boxes: [box], alerts: [], localUsage: nil)
        XCTAssertEqual(model.iconState, .attention)
        XCTAssertEqual(model.boxes[0].lines.map(\.title), [
            "cpu 12% mem 3.4/15 GB load 0.4",
            "agents: 2 working, 1 needs you",
            "ports: 5 open",
            "claude 5h 20% 7d 68%",
            "codex api key"
        ])
        XCTAssertTrue(MenuModelBuilder.renderText(model).contains("Refresh"))
        print("\nSwift menu model after (text, not pixels):\n\(MenuModelBuilder.renderText(model))\n")
    }

    func testMarksUnreachableAndTailnetBoxes() {
        let model = MenuModelBuilder.build(boxes: [
            BoxSnapshot(name: "down", url: "http://down:8100", since: "2026-09-17T22:05:00Z"),
            BoxSnapshot(name: "tail", url: "http://tail:8100", ok: true, discovered: true, tag: "tailnet")
        ], alerts: [], localUsage: nil)

        XCTAssertEqual(model.iconState, .rust)
        XCTAssertEqual(model.boxes[0].lines[0].title, "unreachable since 22:05")
        XCTAssertEqual(model.boxes[1].title, "tail [tailnet]")
    }

    func testQuietHoursCanCrossMidnight() {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(secondsFromGMT: 0)!
        let quiet = QuietHours(enabled: true, start: "22:00", end: "07:00")
        let late = calendar.date(from: DateComponents(year: 2026, month: 9, day: 17, hour: 23))!
        let day = calendar.date(from: DateComponents(year: 2026, month: 9, day: 17, hour: 12))!

        XCTAssertTrue(quiet.contains(late, calendar: calendar))
        XCTAssertFalse(quiet.contains(day, calendar: calendar))
    }

    func testDefaultConfigMatchesGoBarDefaults() {
        let config = BarConfig.default
        XCTAssertEqual(config.refreshSec, 30)
        XCTAssertEqual(config.openWith, "browser")
        XCTAssertFalse(config.notify)
        XCTAssertTrue(config.boxes.isEmpty)
    }

    func testNotificationsOnlyDescribeNewTransitions() {
        let old = FleetSnapshot(boxes: [BoxSnapshot(name: "box", url: "http://box:8100", ok: true)], alerts: [])
        let waiting = Agent(kind: "claude", status: "needs_you")
        let state = BoxState(host: "box", health: Health(), agents: [waiting], ports: [])
        let current = FleetSnapshot(
            boxes: [BoxSnapshot(name: "box", url: "http://box:8100", ok: false, state: state)],
            alerts: [Alert(id: "a1", title: "Review", message: "A review is ready", boxURL: "http://box:8100")]
        )

        let events = NotificationDecider.newEvents(previous: old, current: current)
        XCTAssertEqual(events.map(\.kind), [.alert, .unreachable, .needsYou])
        XCTAssertEqual(NotificationDecider.newEvents(previous: current, current: current), [])
    }

    private func decode<T: Decodable>(_ type: T.Type, named name: String) throws -> T {
        let url = Bundle.module.url(forResource: name, withExtension: "json", subdirectory: "Fixtures")!
        return try JSONDecoder().decode(T.self, from: Data(contentsOf: url))
    }
}
