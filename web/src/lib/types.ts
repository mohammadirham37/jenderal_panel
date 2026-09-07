export interface User {
	id: string;
	username: string;
	email: string;
	is_active: boolean;
	created_at: string;
	updated_at: string;
}

export interface Role {
	id: string;
	name: string;
	description: string;
	created_at: string;
	updated_at: string;
}

export interface Permission {
	id: string;
	name: string;
	module: string;
}

export interface UserWithRoles {
	user: User;
	roles: Role[];
	permissions: Permission[];
}

export interface LoginResponse {
	user: User;
	permissions: Permission[];
	csrf_token: string;
}

export interface ServerInfo {
	hostname: string;
	ip: string;
	os: string;
	kernel: string;
	cpu: string;
	cpu_model: string;
	cpu_cores: number;
	ram: string;
	disk: string;
	uptime: string;
	timezone: string;
	disk_partitions: DiskPartition[];
	network_interfaces: NetworkInterface[];
}

export interface DiskPartition {
	device: string;
	mount: string;
	size: string;
	used: string;
	available: string;
	use_percent: string;
}

export interface NetworkInterface {
	name: string;
	ip: string;
	mac: string;
}

export interface ServerMetrics {
	cpu: number;
	ram_used: number;
	ram_total: number;
	swap_used: number;
	swap_total: number;
	disk_used: number;
	disk_total: number;
	load_1: number;
	load_5: number;
	load_15: number;
	net_rx: number;
	net_tx: number;
	uptime: number;
	timestamp: string;
}

export interface ServiceStatus {
	name: string;
	active: boolean;
	running: boolean;
	enabled: boolean;
	uptime: number;
	pid: number;
}

export interface AuditEntry {
	id: string;
	user_id: string;
	action: string;
	module: string;
	target: string;
	detail: string;
	ip_address: string;
	created_at: string;
}

export interface Setting {
	key: string;
	value: string;
	updated_at: string;
}

export interface ApiResponse<T> {
	data: T;
	meta?: {
		page: number;
		per_page: number;
		total: number;
	};
}

export interface ApiError {
	error: {
		code: string;
		message: string;
	};
}

export interface DashboardData {
	server: ServerInfo;
	metrics: ServerMetrics | null;
	counts: {
		users: number;
		services: number;
	};
}
