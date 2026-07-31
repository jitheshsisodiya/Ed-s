export namespace agent {
	
	export class Network {
	    id: string;
	    name: string;
	    description: string;
	    cidr: string;
	    role: string;
	    memberCount: number;
	    deviceCount: number;
	    inviteCode: string;
	    joined: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Network(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.cidr = source["cidr"];
	        this.role = source["role"];
	        this.memberCount = source["memberCount"];
	        this.deviceCount = source["deviceCount"];
	        this.inviteCode = source["inviteCode"];
	        this.joined = source["joined"];
	    }
	}
	export class Peer {
	    deviceId: string;
	    deviceName: string;
	    virtualIp: string;
	    mode: string;
	    endpoint: string;
	    lastHandshake: string;
	    bytesSent: number;
	    bytesReceived: number;
	
	    static createFrom(source: any = {}) {
	        return new Peer(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.deviceId = source["deviceId"];
	        this.deviceName = source["deviceName"];
	        this.virtualIp = source["virtualIp"];
	        this.mode = source["mode"];
	        this.endpoint = source["endpoint"];
	        this.lastHandshake = source["lastHandshake"];
	        this.bytesSent = source["bytesSent"];
	        this.bytesReceived = source["bytesReceived"];
	    }
	}
	export class Session {
	    loggedIn: boolean;
	    serverUrl: string;
	    email: string;
	    deviceName: string;
	    publicKey: string;
	
	    static createFrom(source: any = {}) {
	        return new Session(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.loggedIn = source["loggedIn"];
	        this.serverUrl = source["serverUrl"];
	        this.email = source["email"];
	        this.deviceName = source["deviceName"];
	        this.publicKey = source["publicKey"];
	    }
	}
	export class Status {
	    connected: boolean;
	    networkId: string;
	    networkName: string;
	    interfaceName: string;
	    virtualIp: string;
	    cidr: string;
	    natType: string;
	    publicEndpoint: string;
	    peers: Peer[];
	
	    static createFrom(source: any = {}) {
	        return new Status(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.connected = source["connected"];
	        this.networkId = source["networkId"];
	        this.networkName = source["networkName"];
	        this.interfaceName = source["interfaceName"];
	        this.virtualIp = source["virtualIp"];
	        this.cidr = source["cidr"];
	        this.natType = source["natType"];
	        this.publicEndpoint = source["publicEndpoint"];
	        this.peers = this.convertValues(source["peers"], Peer);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace main {
	
	export class AppInfo {
	    version: string;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new AppInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.error = source["error"];
	    }
	}

}

