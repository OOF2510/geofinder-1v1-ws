const args = process.argv.slice(2);
const isRender = args.includes("--render");
const isLocal = args.includes("--local");

let rhash: string

const BASE_URL = isLocal
  ? "ws://localhost:8080"
  : "wss://geofinder-1v1-ws.onrender.com";
const API_URL = "https://geo.api.oof2510.space/1v1/new";
const BYPASS_APP_CHECK = process.env.BYPASS_APP_CHECK || "NEED THIS";

const countries = [
  { code: "US", name: "United States" },
  { code: "GB", name: "United Kingdom" },
  { code: "FR", name: "France" },
  { code: "DE", name: "Germany" },
  { code: "JP", name: "Japan" },
  { code: "CN", name: "China" },
  { code: "BR", name: "Brazil" },
  { code: "AU", name: "Australia" },
  { code: "CA", name: "Canada" },
  { code: "IT", name: "Italy" },
  { code: "ES", name: "Spain" },
  { code: "MX", name: "Mexico" },
  { code: "IN", name: "India" },
  { code: "KR", name: "South Korea" },
  { code: "RU", name: "Russia" },
  { code: "ZA", name: "South Africa" },
  { code: "AR", name: "Argentina" },
  { code: "EG", name: "Egypt" },
  { code: "NG", name: "Nigeria" },
  { code: "SE", name: "Sweden" },
];

function randomCountry(): any {
  return countries[Math.floor(Math.random() * countries.length)];
}

async function createRoom(): Promise<string> {
  console.log(`Creating new room via ${API_URL}...`);
  const url = `${API_URL}?bypassAppCheck=${BYPASS_APP_CHECK}`;
  const response = await fetch(url);
  const data = (await response.json()) as { hash: string; ok: boolean };
  if (!data.ok) {
    throw new Error("Failed to create room");
  }
  console.log(`Created room with hash: ${data.hash}`);
  return data.hash;
}

interface WebSocketClient {
  ws: WebSocket;
  role: string;
  playerId: string;
  isAuthenticated: boolean;
  messages: any[];
}

function createClient(url: string, role: string): WebSocketClient {
  const client: WebSocketClient = {
    ws: new WebSocket(url),
    role,
    playerId: "",
    isAuthenticated: false,
    messages: [],
  };

  client.ws.onopen = () => {
    console.log(`[${role}] Connected`);
  };

  client.ws.onmessage = (event: MessageEvent) => {
    try {
      const parsed = JSON.parse(event.data);
      client.messages.push(parsed);
      console.log(`[${role}] Received:`, JSON.stringify(parsed, null, 2));

      if (parsed.type === "auth_ok" && !client.isAuthenticated) {
        client.isAuthenticated = true;
        client.playerId = parsed.playerId;
        console.log(
          `[${role}] Authenticated as ${parsed.role} with ID ${client.playerId}`,
        );
      }

      if (parsed.type === "round_start") {
        setTimeout(
          () => {
            const guess = randomCountry();
            console.log(
              `[${role}] Submitting guess: ${guess.name} (${guess.code})`,
            );
            client.ws.send(
              JSON.stringify({
                event: "submit_answer",
                data: {
                  hash: rhash,
                  playerId: client.playerId,
                  countryCode: guess.code,
                  countryName: guess.name,
                },
              }),
            );
          },
          Math.random() * 20000 + 5000,
        );
      }

      if (parsed.type === "game_end") {
        console.log(`[${role}] Game ended! Winner: ${parsed.winner}`);
        console.log(
          `[${role}] Final Score - Host: ${parsed.hostScore}, Guest: ${parsed.guestScore}`,
        );
        setTimeout(() => {
          client.ws.close();
        }, 2000);
      }
    } catch {
      console.log(`[${role}] Raw message:`, event.data);
    }
  };

  client.ws.onerror = (error: Event) => {
    console.error(`[${role}] WebSocket error:`, error);
  };

  client.ws.onclose = (event: CloseEvent) => {
    console.log(`[${role}] Connection closed: ${event.code} - ${event.reason}`);
  };

  return client;
}

async function runTest() {
  console.log("=== GeoFinder 1v1 WebSocket Test ===");
  console.log(
    `Mode: ${isRender ? "Render" : isLocal ? "Local" : "Default (Render)"}`,
  );
  console.log(`Base URL: ${BASE_URL}`);
  console.log("");

  const roomHash = await createRoom();
  rhash = roomHash
  console.log("");

  const hostWsUrl = `${BASE_URL}/ws?roomHash=${roomHash}`;
  const guestWsUrl = `${BASE_URL}/ws?roomHash=${roomHash}`;

  const host = createClient(hostWsUrl, "host");
  const guest = createClient(guestWsUrl, "guest");

  setTimeout(() => {
    console.log("\n=== Sending auth messages ===");
    host.ws.send(
      JSON.stringify({
        event: "auth",
        data: { hash: roomHash },
      }),
    );
    guest.ws.send(
      JSON.stringify({
        event: "auth",
        data: { hash: roomHash },
      }),
    );
  }, 1000);

  setTimeout(() => {
    if (!host.isAuthenticated || !guest.isAuthenticated) {
      console.log("Waiting for authentication...");
    }
  }, 3000);

  setTimeout(() => {
    console.log("\n=== Test completed ===");
    process.exit(0);
  }, 120000);
}

runTest().catch(console.error);
