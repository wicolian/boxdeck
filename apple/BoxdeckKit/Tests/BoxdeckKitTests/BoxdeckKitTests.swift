import XCTest
@testable import BoxdeckKit

final class BoxdeckKitTests: XCTestCase {
    func testDecodesGoStateFixture() throws {
        let data = Data(#"{"host":"box","health":{"cpu":12,"memUsed":3650722201,"memTotal":16106127360,"load1":0.4},"agents":[{"kind":"claude","agent_status":"working"},{"kind":"codex","agent_status":"needs_you"}],"ports":[{"port":3000},{"port":3001}]}"#.utf8)
        let state = try JSONDecoder().decode(BoxState.self, from: data)
        XCTAssertEqual(state.host, "box")
        XCTAssertEqual(state.health.cpu, 12)
        XCTAssertEqual(state.agents[1].status, "needs_you")
        XCTAssertEqual(state.ports.count, 2)
    }

    func testBuildsMenuModelWithNeedsYouAndQuota() throws {
        let box = BoxSnapshot(name: "box", url: "http://box:8100", ok: true, health: HealthSnapshot(cpu: 12, memUsed: 3_650_722_201, memTotal: 16_106_127_360, load1: 0.4), agents: 2, ports: 5, usage: BoxUsage(quota: ["claude": Quota(fiveHour: QuotaWindow(pct: 20), sevenDay: QuotaWindow(pct: 68))]))
        let state = BoxState(agents: [Agent(status: "working"), Agent(status: "needs_you")])
        let model = MenuModelBuilder.build(boxes: [box], states: [box.url: state])
        XCTAssertEqual(model.boxes.first?.agents, "1 working, 1 needs you")
        XCTAssertEqual(model.boxes.first?.quota.first, "claude 5h 20% 7d 68%")
    }

    func testParsesPairingURL() throws {
        let pairing = try PairingURL.parse(URL(string: "boxdeck://add?url=http%3A%2F%2Fbox%3A8100&token=bd_test")!)
        XCTAssertEqual(pairing.label, "phone")
        XCTAssertEqual(pairing.url, "http://box:8100")
        XCTAssertEqual(pairing.token, "bd_test")
    }

    func testQuietHoursCrossesMidnight() {
        let quiet = QuietHours(from: "23:00", to: "08:00", allowCritical: true)
        let calendar = Calendar(identifier: .gregorian)
        let late = calendar.date(from: DateComponents(year: 2026, month: 9, day: 17, hour: 23, minute: 30))!
        let day = calendar.date(from: DateComponents(year: 2026, month: 9, day: 17, hour: 12))!
        XCTAssertTrue(quiet.contains(late, calendar: calendar))
        XCTAssertFalse(quiet.contains(day, calendar: calendar))
        XCTAssertTrue(NotificationPolicy.shouldDeliver(severity: "critical", quiet: quiet, disarmed: false, date: late))
        XCTAssertFalse(NotificationPolicy.shouldDeliver(severity: "warning", quiet: quiet, disarmed: false, date: late))
    }
}
