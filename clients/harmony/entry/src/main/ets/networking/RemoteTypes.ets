export interface RemoteUser { id: string; name: string; }
export interface RemoteDevice { id: string; userId: string; name: string; }
export interface Connection { device: RemoteDevice; user: RemoteUser; token: string; }
export interface Settings { server: string; deviceName: string; connection: Connection; }
export interface Attachment { id: string; name: string; mime: string; size: number; width: number; height: number; }
export interface RemoteEntry {
  id: string;
  userId: string;
  senderId: string;
  sourceDeviceId: string;
  kind: string;
  text: string;
  createdAt: number;
  attachment?: Attachment;
}
export interface RemoteEvent { type: string; itemId?: string; sourceDeviceId?: string; }

// IDs are decimal strings on the wire, avoiding the JavaScript 2^53 precision limit.
export function newer(a: string, b: string): boolean {
  return a.length > b.length || (a.length === b.length && a > b);
}
