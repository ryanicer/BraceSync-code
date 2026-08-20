const http = require("http");
const { URL } = require("url");

// CORS headers — 简单跨域 API 默认放行所有来源
const CORS_HEADERS = {
  "Access-Control-Allow-Origin": "*",
  "Access-Control-Allow-Methods": "GET, POST, OPTIONS",
  "Access-Control-Allow-Headers": "Content-Type",
};

function sendJson(res, statusCode, data) {
  res.writeHead(statusCode, {
    "Content-Type": "application/json; charset=utf-8",
    ...CORS_HEADERS,
  });
  res.end(JSON.stringify(data));
}

function sendOptions(res) {
  res.writeHead(204, CORS_HEADERS);
  res.end();
}

function readJsonBody(req) {
  return new Promise((resolve, reject) => {
    let raw = "";
    req.on("data", (chunk) => {
      raw += chunk;
    });
    req.on("end", () => {
      if (!raw) {
        resolve({});
        return;
      }
      try {
        resolve(JSON.parse(raw));
      } catch (error) {
        reject(new Error("Invalid JSON body"));
      }
    });
    req.on("error", reject);
  });
}

const server = http.createServer(async (req, res) => {
  // 处理 CORS 预检请求
  if (req.method === "OPTIONS") {
    return sendOptions(res);
  }

  const url = new URL(req.url || "/", "http://127.0.0.1");
  console.log(`[hello-http] ${req.method} ${url.pathname}`);

  if (req.method === "GET" && url.pathname === "/") {
    console.log("[hello-http] handling GET /");
    sendJson(res, 200, {
      ok: true,
      message: "hello from http function",
      time: new Date().toISOString(),
    });
    return;
  }

  if (req.method === "GET" && url.pathname === "/health") {
    sendJson(res, 200, { ok: true, service: "hello-http", status: "healthy" });
    return;
  }

  if (req.method === "POST" && url.pathname === "/echo") {
    try {
      const body = await readJsonBody(req);
      console.log("[hello-http] echo body:", JSON.stringify(body));
      sendJson(res, 200, { received: body });
    } catch (error) {
      sendJson(res, 400, { error: error.message });
    }
    return;
  }

  sendJson(res, 404, { error: "Not Found" });
});

server.listen(9000, () => {
  console.log("[hello-http] HTTP function listening on port 9000");
});
