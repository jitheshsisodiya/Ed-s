export interface User {
  id: string;
  email: string;
  displayName: string;
  mfaEnabled: boolean;
  status: "active" | "disabled" | "pending_verification";
  createdAt: string;
}

export interface TokenPair {
  accessToken: string;
  refreshToken: string;
  expiresIn: number;
  mfaRequired?: boolean;
}

export type NetworkRole = "owner" | "admin" | "member";

export interface Network {
  id: string;
  name: string;
  description: string;
  cidr: string;
  dnsServers: string[];
  role: NetworkRole;
  memberCount: number;
  deviceCount: number;
  inviteCode: string;
  createdAt: string;
}

export interface Member {
  userId: string;
  email: string;
  displayName: string;
  role: NetworkRole;
  joinedAt: string;
}

export type DeviceStatus = "online" | "offline" | "unknown";

export interface Device {
  id: string;
  name: string;
  os: string;
  osVersion: string;
  publicKey: string;
  virtualIp: string;
  lastPublicIp?: string;
  status: DeviceStatus;
  natType?: string;
  latencyMs?: number;
  bytesSent: number;
  bytesReceived: number;
  lastSeenAt?: string;
}

export interface ConnectionLog {
  id: string;
  deviceId: string;
  peerDeviceId?: string;
  eventType: string;
  latencyMs?: number;
  createdAt: string;
}

export interface AuditLog {
  id: string;
  actorUserId?: string;
  action: string;
  targetType?: string;
  targetId?: string;
  createdAt: string;
}

export interface DashboardStats {
  activeUsers: number;
  activeNetworks: number;
  onlineDevices: number;
  totalDevices: number;
  relayBandwidthBytes: number;
  p2pConnectionRatio: number;
}

export class NexusVPNAPIError extends Error {
  constructor(
    public readonly code: string,
    message: string,
    public readonly status: number,
  ) {
    super(message);
    this.name = "NexusVPNAPIError";
  }
}
