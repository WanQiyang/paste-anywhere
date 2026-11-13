namespace PasteAnywhere.Networking;

// Each RemoteClient owns its cache, so bytes never cross server/credential sessions.
internal sealed class ImageDownloadCache
{
    private sealed record Item(string Key, byte[] Bytes, DateTime Created);
    private readonly LinkedList<Item> items = new();
    private long size;

    public byte[]? Get(string key, long expectedSize)
    {
        lock (items)
        {
            var node = items.First;
            while (node != null)
            {
                var next = node.Next;
                if (DateTime.UtcNow - node.Value.Created > TimeSpan.FromDays(7)) Remove(node);
                else if (node.Value.Key == key)
                {
                    if (node.Value.Bytes.LongLength != expectedSize) { Remove(node); return null; }
                    items.Remove(node); items.AddLast(node);
                    return node.Value.Bytes;
                }
                node = next;
            }
            return null;
        }
    }

    public void Forget(string key)
    {
        lock (items)
        {
            for (var node = items.First; node != null; node = node.Next)
                if (node.Value.Key == key) { Remove(node); break; }
        }
    }

    public void Put(string key, byte[] bytes)
    {
        lock (items)
        {
            Forget(key);
            items.AddLast(new Item(key, bytes, DateTime.UtcNow)); size += bytes.LongLength;
            while (items.Count > 20 || size > 64 * 1024 * 1024) Remove(items.First!);
        }
    }

    public void Clear() { lock (items) { items.Clear(); size = 0; } }
    private void Remove(LinkedListNode<Item> node) { size -= node.Value.Bytes.LongLength; items.Remove(node); }
}
