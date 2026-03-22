import WebSocket from "ws";
import { spawn } from "child_process";
import http from "http";

export interface DaemonMessage {
  type: string;
  request_id?: string;
  payload: any;
}

type MessageHandler = (msg: DaemonMessage) => void;

export class DaemonClient {
  private ws: WebSocket | null = null;
  private url: string;
  private handlers: Map<string, MessageHandler[]> = new Map();
  private pendingRequests: Map<string, { resolve: (msg: DaemonMessage) => void; reject: (err: Error) => void }> =
    new Map();
  private reconnecting = false;
  private closed = false;
  private requestCounter = 0;
  private connectPromise: Promise<void> | null = null;

  constructor(port = 7390) {
    this.url = `ws://127.0.0.1:${port}/ws`;
  }

  async connect(): Promise<void> {
    if (this.connectPromise) return this.connectPromise;
    this.connectPromise = this.doConnect();
    return this.connectPromise;
  }

  private async doConnect(): Promise<void> {
    try {
      await this.tryConnect();
    } catch {
      // Auto-launch daemon
      await this.launchDaemon();
      await this.tryConnect();
    }
  }

  private tryConnect(): Promise<void> {
    return new Promise((resolve, reject) => {
      const ws = new WebSocket(this.url);

      ws.on("open", () => {
        this.ws = ws;
        this.reconnecting = false;
        resolve();
      });

      ws.on("message", (data: WebSocket.Data) => {
        try {
          const msg: DaemonMessage = JSON.parse(data.toString());

          // Check for pending request/response correlation
          if (msg.request_id && this.pendingRequests.has(msg.request_id)) {
            const pending = this.pendingRequests.get(msg.request_id)!;
            this.pendingRequests.delete(msg.request_id);
            pending.resolve(msg);
            return;
          }

          // Dispatch to type handlers
          const handlers = this.handlers.get(msg.type);
          if (handlers) {
            for (const h of handlers) h(msg);
          }
        } catch (err) {
          console.error("[daemon-client] parse error:", err);
        }
      });

      ws.on("close", () => {
        this.ws = null;
        if (!this.closed) {
          this.reconnectWithBackoff();
        }
      });

      ws.on("error", (err: Error) => {
        if (!this.ws) {
          reject(err);
        }
      });
    });
  }

  private async launchDaemon(): Promise<void> {
    const child = spawn("commons", ["server", "start", "--foreground"], {
      detached: true,
      stdio: "ignore",
    });
    child.unref();

    // Poll /health for up to 6 seconds
    const port = 7390;
    for (let i = 0; i < 12; i++) {
      await sleep(500);
      try {
        await httpGet(`http://127.0.0.1:${port}/health`);
        return;
      } catch {
        // retry
      }
    }
    throw new Error("Daemon failed to start after 6s");
  }

  private async reconnectWithBackoff(): Promise<void> {
    if (this.reconnecting || this.closed) return;
    this.reconnecting = true;

    let delay = 500;
    for (let attempt = 0; attempt < 12; attempt++) {
      if (this.closed) return;
      const jitter = delay * (0.75 + Math.random() * 0.5);
      await sleep(jitter);
      try {
        await this.tryConnect();
        return;
      } catch {
        delay = Math.min(delay * 2, 30000);
      }
    }
    console.error("[daemon-client] reconnection failed after 12 attempts");
  }

  on(type: string, handler: MessageHandler): void {
    if (!this.handlers.has(type)) {
      this.handlers.set(type, []);
    }
    this.handlers.get(type)!.push(handler);
  }

  async send(type: string, payload: any): Promise<DaemonMessage> {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
      throw new Error("Not connected to daemon");
    }

    const requestId = `req-${++this.requestCounter}`;
    const msg: DaemonMessage = { type, request_id: requestId, payload };

    return new Promise((resolve, reject) => {
      this.pendingRequests.set(requestId, { resolve, reject });
      this.ws!.send(JSON.stringify(msg), (err) => {
        if (err) {
          this.pendingRequests.delete(requestId);
          reject(err);
        }
      });

      // Timeout after 10s
      setTimeout(() => {
        if (this.pendingRequests.has(requestId)) {
          this.pendingRequests.delete(requestId);
          reject(new Error(`Request ${requestId} timed out`));
        }
      }, 10000);
    });
  }

  sendFire(type: string, payload: any): void {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return;
    const requestId = `req-${++this.requestCounter}`;
    this.ws.send(JSON.stringify({ type, request_id: requestId, payload }));
  }

  close(): void {
    this.closed = true;
    if (this.ws) {
      this.ws.close();
      this.ws = null;
    }
  }

  get isConnected(): boolean {
    return this.ws !== null && this.ws.readyState === WebSocket.OPEN;
  }
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function httpGet(url: string): Promise<string> {
  return new Promise((resolve, reject) => {
    http
      .get(url, (res) => {
        let data = "";
        res.on("data", (chunk) => (data += chunk));
        res.on("end", () => resolve(data));
      })
      .on("error", reject);
  });
}
