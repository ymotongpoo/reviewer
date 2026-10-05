export type DraftStoreName = 'composer' | 'edit'

export interface DraftStore {
  readonly persistent: boolean
  get<T>(store: DraftStoreName, key: string): Promise<T | undefined>
  put<T>(store: DraftStoreName, key: string, value: T): Promise<void>
  del(store: DraftStoreName, key: string): Promise<void>
  list<T>(store: DraftStoreName, prefix: string): Promise<T[]>
}

export function memoryDraftStore(): DraftStore {
  const stores = { composer: new Map<string, unknown>(), edit: new Map<string, unknown>() }
  return {
    persistent: false,
    async get<T>(store: DraftStoreName, key: string) { return structuredClone(stores[store].get(key)) as T | undefined },
    async put(store, key, value) { stores[store].set(key, structuredClone(value)) },
    async del(store, key) { stores[store].delete(key) },
    async list<T>(store: DraftStoreName, prefix: string) {
      return [...stores[store]].filter(([key]) => key.startsWith(prefix)).map(([, value]) => structuredClone(value) as T)
    },
  }
}

export function createDraftStore(onUnavailable: () => void): DraftStore {
  const memory = memoryDraftStore()
  let unavailable = false
  let database: Promise<IDBDatabase> | undefined
  let queue: Promise<unknown> = Promise.resolve()
  function open() {
    return database ??= new Promise<IDBDatabase>((resolve, reject) => {
      const request = indexedDB.open('reviewer-drafts', 1)
      request.onupgradeneeded = () => {
        request.result.createObjectStore('composer')
        request.result.createObjectStore('edit')
      }
      request.onerror = () => reject(request.error)
      request.onblocked = () => reject(new Error('Draft database is blocked'))
      request.onsuccess = () => {
        if (unavailable) { request.result.close(); return }
        request.result.onversionchange = () => request.result.close()
        resolve(request.result)
      }
    })
  }
  function transaction<T>(db: IDBDatabase, store: DraftStoreName, mode: IDBTransactionMode, action: (s: IDBObjectStore) => IDBRequest<T>) {
    return new Promise<T>((resolve, reject) => {
      const tx = db.transaction(store, mode)
      const request = action(tx.objectStore(store))
      tx.oncomplete = () => resolve(request.result)
      tx.onabort = tx.onerror = () => reject(tx.error ?? request.error)
    })
  }
  // Serialize transactions and shadow writes so fallback preserves their order.
  function run<T>(action: (db: IDBDatabase) => Promise<T>, fallback: () => Promise<T>): Promise<T> {
    const next = queue.then(async () => {
      if (!unavailable) {
        try { return await action(await open()) }
        catch {
          unavailable = true
          onUnavailable()
        }
      }
      return fallback()
    })
    queue = next.catch(() => {})
    return next
  }
  return {
    get persistent() { return !unavailable },
    get<T>(store: DraftStoreName, key: string) {
      return run(async (db) => {
        const value = await transaction<T | undefined>(db, store, 'readonly', (s) => s.get(key))
        if (value !== undefined) await memory.put(store, key, value)
        else await memory.del(store, key)
        return value
      }, () => memory.get<T>(store, key))
    },
    put(store, key, value) {
      return run(async (db) => {
        await transaction(db, store, 'readwrite', (s) => s.put(value, key))
        await memory.put(store, key, value)
      }, () => memory.put(store, key, value))
    },
    del(store, key) {
      return run(async (db) => {
        await transaction(db, store, 'readwrite', (s) => s.delete(key))
        await memory.del(store, key)
      }, () => memory.del(store, key))
    },
    list<T>(store: DraftStoreName, prefix: string) {
      return run(async (db) => {
        // Read keys as well to retain values in the memory fallback without assuming a value schema.
        const entries = await new Promise<Array<[string, T]>>((resolve, reject) => {
          const tx = db.transaction(store, 'readonly')
          const entries: Array<[string, T]> = []
          const cursor = tx.objectStore(store).openCursor(IDBKeyRange.bound(prefix, prefix + '\uffff'))
          cursor.onsuccess = () => {
            const c = cursor.result
            if (c) { entries.push([String(c.key), c.value]); c.continue() }
          }
          tx.oncomplete = () => resolve(entries)
          tx.onabort = tx.onerror = () => reject(tx.error ?? cursor.error)
        })
        for (const [key, value] of entries) await memory.put(store, key, value)
        return entries.map(([, value]) => value)
      }, () => memory.list<T>(store, prefix))
    },
  }
}
