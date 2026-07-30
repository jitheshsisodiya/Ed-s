// TypeScript interfaces hand-written to mirror the schemas defined in
// api/openapi.yaml. Keep these in sync with that file.

export type UserStatus = 'active' | 'disabled' | 'pending_verification';

export interface User {
  id: string;
  email: string;
  displayName: string;
  mfaEnabled: boolean;
  status: UserStatus;
  createdAt: string;
}

export interface RegisterRequest {
  email: string;
  password: string;
  displayName: string;
}

export interface LoginRequest {
  email: string;
  password: string;
  mfaCode?: string;
}

export interface TokenPair {
  accessToken: string;
  refreshToken: string;
  expiresIn: number;
  mfaRequired?: boolean;
}

export type NetworkRole = 'owner' | 'admin' | 'member';

export interface Network {
  id: string;
  name: string;
  description?: string;
  cidr: string;
  dnsServers: string[];
  role: NetworkRole;
  memberCount: number;
  deviceCount: number;
  inviteCode?: string;
  createdAt: string;
}

export interface NetworkCreateRequest {
  name: string;
  description?: string;
  cidr: string;
  dnsServers?: string[];
}

export interface JoinNetworkRequest {
  inviteCode: string;
}

export interface RotateInviteResponse {
  inviteCode: string;
  expiresAt: string;
}

export interface Member {
  userId: string;
  email: string;
  displayName: string;
  role: NetworkRole;
  joinedAt: string;
}

export type DeviceStatus = 'online' | 'offline' | 'unknown';

export interface Device {
  id: string;
  name: string;
  os: string;
  osVersion?: string;
  publicKey: string;
  virtualIp: string;
  lastPublicIp?: string;
  status: DeviceStatus;
  natType?: string;
  latencyMs?: number;
  bytesSent: number;
  bytesReceived: number;
  lastSeenAt: string;
  // Populated client-side when flattening cross-network device listings;
  // not part of the base OpenAPI Device schema.
  networkId?: string;
  networkName?: string;
}

export interface DeviceRegisterRequest {
  name: string;
  os: string;
  osVersion?: string;
  publicKey: string;
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
  actorUserId: string;
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

export interface MfaEnableResponse {
  secret: string;
  otpauthUrl: string;
}

export interface ApiErrorBody {
  error: {
    code: string;
    message: string;
  };
}
