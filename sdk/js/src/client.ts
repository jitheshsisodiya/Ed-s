import {
  AuditLog,
  ConnectionLog,
  DashboardStats,
  Device,
  Member,
  Network,
  NexusVPNAPIError,
  TokenPair,
  User,
} from "./types.js";

export interface ClientOptions {
  baseUrl: string;
  accessToken?: string;
  refreshToken?: string;
  fetchImpl?: typeof fetch;
  onTokensRefreshed?: (tokens: TokenPair) => void;
}

/** Typed client for the NexusVPN control-plane REST API (api/openapi.yaml). */
export class NexusVPNClient {
  private baseUrl: string;
  private accessToken?: string;
  private refreshToken?: string;
  private fetchImpl: typeof fetch;
  private onTokensRefreshed?: (tokens: TokenPair) => void;

  constructor(opts: ClientOptions) {
    this.baseUrl = opts.baseUrl.replace(/\/+$/, "");
    this.accessToken = opts.accessToken;
    this.refreshToken = opts.refreshToken;
    this.fetchImpl = opts.fetchImpl ?? fetch;
    this.onTokensRefreshed = opts.onTokensRefreshed;
  }

  setTokens(tokens: TokenPair) {
    this.accessToken = tokens.accessToken;
    if (tokens.refreshToken) this.refreshToken = tokens.refreshToken;
  }

  private async request<T>(
    method: string,
    path: string,
    body?: unknown,
    allowRefresh = true,
  ): Promise<T> {
    const headers: Record<string, string> = { "Content-Type": "application/json" };
    if (this.accessToken) headers.Authorization = `Bearer ${this.accessToken}`;

    const res = await this.fetchImpl(`${this.baseUrl}${path}`, {
      method,
      headers,
      body: body !== undefined ? JSON.stringify(body) : undefined,
    });

    if (res.status === 401 && allowRefresh && this.refreshToken) {
      try {
        const tokens = await this.request<TokenPair>(
          "POST",
          "/auth/refresh",
          { refreshToken: this.refreshToken },
          false,
        );
        this.setTokens(tokens);
        this.onTokensRefreshed?.(tokens);
        return this.request<T>(method, path, body, false);
      } catch {
        // fall through to normal error handling below
      }
    }

    if (!res.ok) {
      let code = "unknown_error";
      let message = res.statusText;
      try {
        const parsed = await res.json();
        code = parsed?.error?.code ?? code;
        message = parsed?.error?.message ?? message;
      } catch {
        // ignore non-JSON error bodies
      }
      throw new NexusVPNAPIError(code, message, res.status);
    }

    if (res.status === 204) return undefined as T;
    const text = await res.text();
    return text ? (JSON.parse(text) as T) : (undefined as T);
  }

  // --- Auth ---
  register(email: string, password: string, displayName: string) {
    return this.request<User>("POST", "/auth/register", { email, password, displayName });
  }

  async login(email: string, password: string, mfaCode?: string) {
    const tokens = await this.request<TokenPair>("POST", "/auth/login", { email, password, mfaCode });
    this.setTokens(tokens);
    return tokens;
  }

  logout() {
    return this.request<void>("POST", "/auth/logout");
  }

  forgotPassword(email: string) {
    return this.request<void>("POST", "/auth/password/forgot", { email });
  }

  resetPassword(token: string, newPassword: string) {
    return this.request<void>("POST", "/auth/password/reset", { token, newPassword });
  }

  enableMfa() {
    return this.request<{ secret: string; otpauthUrl: string }>("POST", "/auth/mfa/enable");
  }

  verifyMfa(code: string) {
    return this.request<void>("POST", "/auth/mfa/verify", { code });
  }

  // --- Networks ---
  listNetworks() {
    return this.request<Network[]>("GET", "/networks");
  }

  createNetwork(input: { name: string; description?: string; cidr: string; dnsServers?: string[] }) {
    return this.request<Network>("POST", "/networks", input);
  }

  joinNetwork(inviteCode: string) {
    return this.request<Network>("POST", "/networks/join", { inviteCode });
  }

  getNetwork(networkId: string) {
    return this.request<Network>("GET", `/networks/${networkId}`);
  }

  updateNetwork(networkId: string, input: Partial<{ name: string; description: string; dnsServers: string[] }>) {
    return this.request<Network>("PATCH", `/networks/${networkId}`, input);
  }

  deleteNetwork(networkId: string) {
    return this.request<void>("DELETE", `/networks/${networkId}`);
  }

  rotateInvite(networkId: string) {
    return this.request<{ inviteCode: string; expiresAt: string }>("POST", `/networks/${networkId}/invite`);
  }

  listMembers(networkId: string) {
    return this.request<Member[]>("GET", `/networks/${networkId}/members`);
  }

  updateMemberRole(networkId: string, userId: string, role: "admin" | "member") {
    return this.request<void>("PATCH", `/networks/${networkId}/members/${userId}`, { role });
  }

  removeMember(networkId: string, userId: string) {
    return this.request<void>("DELETE", `/networks/${networkId}/members/${userId}`);
  }

  // --- Devices ---
  listDevices(networkId: string) {
    return this.request<Device[]>("GET", `/networks/${networkId}/devices`);
  }

  registerDevice(networkId: string, input: { name: string; os: string; osVersion?: string; publicKey: string }) {
    return this.request<Device>("POST", `/networks/${networkId}/devices`, input);
  }

  getDevice(deviceId: string) {
    return this.request<Device>("GET", `/devices/${deviceId}`);
  }

  deleteDevice(deviceId: string) {
    return this.request<void>("DELETE", `/devices/${deviceId}`);
  }

  deviceHeartbeat(deviceId: string) {
    return this.request<void>("POST", `/devices/${deviceId}/heartbeat`);
  }

  listPeers(deviceId: string) {
    return this.request<Device[]>("GET", `/devices/${deviceId}/peers`);
  }

  // --- Dashboard / logs ---
  dashboardStats() {
    return this.request<DashboardStats>("GET", "/dashboard/stats");
  }

  auditLogs(networkId: string) {
    return this.request<AuditLog[]>("GET", `/logs/audit?networkId=${networkId}`);
  }

  connectionLogs(networkId: string) {
    return this.request<ConnectionLog[]>("GET", `/logs/connections?networkId=${networkId}`);
  }
}
