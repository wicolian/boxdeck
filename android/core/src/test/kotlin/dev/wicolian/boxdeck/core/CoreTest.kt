package dev.wicolian.boxdeck.core

import kotlinx.serialization.json.Json
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import java.time.LocalTime

class CoreTest {
    private val json = Json { ignoreUnknownKeys = true; explicitNulls = false }
    private lateinit var server: MockWebServer

    @Before
    fun startServer() {
        server = MockWebServer()
        server.start()
    }

    @After
    fun stopServer() {
        server.shutdown()
    }

    private fun fixture(name: String): String = javaClass.classLoader!!.getResource("testdata/$name")!!.readText()

    @Test
    fun decodesGoFixturesAndBuildsAttentionModel() {
        val state = json.decodeFromString(State.serializer(), fixture("state.json"))
        val usage = json.decodeFromString(UsageResponse.serializer(), fixture("usage.json"))
        val boxes = json.decodeFromString<List<BoxSnapshot>>(fixture("boxes.json"))
        val peers = json.decodeFromString<List<TailnetPeer>>(fixture("peers.json"))

        assertEquals(12.0, state.health.cpu, 0.0)
        assertEquals("needs_you", state.agents[1].effectiveStatus)
        assertEquals(515654016L, usage.providers.getValue("claude").today.tokens.total)
        assertEquals(4, boxes[1].agents)
        assertEquals("android", peers[1].os)

        val model = buildMenuModel(boxes.map { it.copy(state = state) })
        assertEquals("attention", model.iconState)
        assertEquals("cpu 12% mem 3.4/15 GB load 0.4", model.boxes[0].lines[0].title)
        assertEquals("agents: 2 working, 1 needs you", model.boxes[0].lines[1].title)
        assertEquals("claude 5h 20% 7d 68%", model.boxes[0].lines[3].title)
    }

    @Test
    fun quietHoursHandlesNormalAndOvernightWindows() {
        assertTrue(QuietHours.isQuiet(LocalTime.of(23, 30), "23:00", "08:00"))
        assertTrue(QuietHours.isQuiet(LocalTime.of(7, 59), "23:00", "08:00"))
        assertFalse(QuietHours.isQuiet(LocalTime.of(12, 0), "23:00", "08:00"))
        assertTrue(QuietHours.isQuiet(LocalTime.of(12, 0), "12:00", "12:00"))
    }

    @Test
    fun clientAddsBearerAndReadsPeers() = kotlinx.coroutines.test.runTest {
        server.enqueue(MockResponse().setResponseCode(200).setBody(fixture("peers.json")))
        val client = BoxdeckClient(server.url("/").toString(), "secret")
        val peers = client.peers()
        assertEquals(3, peers.size)
        val request = server.takeRequest()
        assertEquals("Bearer secret", request.getHeader("Authorization"))
        assertEquals("/api/net/peers", request.path)
    }
}
