# **golapis: A Go-based, High-Concurrency Application Server with Embedded LuaJIT**

## **1\. Introduction**

### **1.1. Purpose**

This document outlines the architectural design for a high-performance, scalable application server built in Go. The server will embed the LuaJIT runtime to allow for dynamic, scriptable request handling. The design is heavily inspired by the proven, high-concurrency model of OpenResty, but it replaces the Nginx master-worker process architecture with a more modern, single-process, multi-goroutine model native to Go.

### **1.2. Goals**

* **High Concurrency:** The system must be capable of handling tens of thousands of simultaneous, long-lived connections efficiently.  
* **High Performance:** Leverage the speed of Go's compiled networking stack and LuaJIT's renowned Just-In-Time (JIT) compiler for script execution.  
* **Dynamic Logic:** Enable developers to implement complex request handling, routing, and business logic in Lua without recompiling the core Go server.  
* **Architectural Simplicity:** By operating within a single Go process, the design aims to simplify state management and inter-worker communication compared to traditional multi-process models.  
* **Compatible with OpenResty application code:** A large subset of the lua-nginx-module will be implemented so that Lua applications written for OpenResty will also be able to be hosted with golapis

## **2\. Core Architecture**

The architecture is built on three synergistic layers: a Go-based worker pool, per-worker LuaJIT virtual machines, and a request dispatching system. This model runs entirely within a single Go process, eliminating the need for traditional OS-level process management.

### **2.1. Single-Process, Multi-Goroutine "Worker" Model**

Instead of Nginx's master-worker process model, this design employs a "worker pool" of long-lived goroutines.1

* **Manager Goroutine:** Upon application startup, a main "manager" goroutine is responsible for initializing the system and spawning a pool of worker goroutines.  
* **Worker Goroutines:** A configurable number of worker goroutines will be created. To maximize hardware utilization and minimize CPU context-switching, this number will default to runtime.NumCPU(). Each worker goroutine operates independently to process incoming requests.  
* **Fault Tolerance:** If a worker goroutine encounters a panic (e.g., from a misbehaving script), it can be caught at the goroutine boundary. The manager can then log the error and spawn a new worker to replace it, ensuring the server remains operational.

### **2.2. Per-Worker LuaJIT VM**

Each worker goroutine will host its own dedicated, persistent LuaJIT Virtual Machine (VM) instance, also known as a Lua State.3 This is a direct parallel to OpenResty's per-worker VM model and is crucial for both performance and memory efficiency.

* **LuaJIT Integration:** The LuaJIT source code is bundled with the go source code and compiled directly into the go binary  
* **VM Lifecycle:** A Lua VM will be created and initialized when its parent worker goroutine starts. This VM will persist for the entire lifetime of the worker, serving all requests assigned to it. It will be closed only when the worker goroutine is terminated.  
* **Code Caching:** This model provides implicit code caching. Lua modules loaded via require() will be parsed and compiled into LuaJIT bytecode only once per worker. The compiled code is then cached within that worker's Lua VM, and all subsequent requests handled by that worker will execute the cached bytecode directly. This amortization of compilation costs is a cornerstone of the system's performance, mirroring the behavior of OpenResty's lua\_code\_cache directive.6

### **2.3. Request Dispatching**

A central listener goroutine will handle incoming network connections and distribute them among the worker pool.

1. The main goroutine will create a TCP listener (e.g., net.Listen) on the configured address and port.  
2. A dedicated listener goroutine will run in a loop, calling listener.Accept() to accept new client connections.  
3. A shared Go channel will serve as a work queue, connecting the listener to the worker goroutines.  
4. Upon accepting a new connection, the listener goroutine will send the net.Conn object to the work queue channel.  
5. The worker goroutines will be concurrently reading from this channel. When a worker receives a connection, it becomes responsible for handling that request for its entire lifecycle.

## **3\. Concurrency Model: Cooperative Multitasking**

To handle thousands of requests within each worker without blocking, the system will implement a cooperative multitasking model using Lua coroutines, directly analogous to the core concurrency mechanism in OpenResty.

### **3.1. Per-Request Lua Coroutines**

When a worker goroutine receives a request, it will not process it directly in the main body of the goroutine. Instead, it will spawn a new, dedicated Lua coroutine to handle that specific request.

* **Isolation:** Each Lua coroutine runs in its own sandboxed environment with a private global variable scope. This ensures that concurrent requests handled by the same worker cannot interfere with or corrupt each other's state.3  
* **Lightweight Nature:** Lua coroutines are extremely lightweight compared to OS threads, with minimal creation overhead and memory footprint, making it feasible to create one for every single request.

### **3.2. The Yield/Resume Cycle for Non-Blocking I/O**

The key to multiplexing many requests onto a single worker goroutine is ensuring that no operation ever blocks the goroutine's underlying OS thread. Since standard Lua I/O functions are blocking, the system must provide a suite of custom, non-blocking APIs that integrate Lua coroutines with Go's native asynchronous I/O. This is the equivalent of OpenResty's cosocket API.

When a Lua script needs to perform an I/O operation (e.g., a network read or a sleep), it will call a Go-provided API (e.g., go.socket.read() or go.sleep()). This will trigger the following non-blocking sequence:

1. The Go function backing the API is invoked from Lua.  
2. The Go function initiates the I/O operation using Go's standard non-blocking network primitives.  
3. Crucially, the Go function then immediately calls lua\_yield(), which suspends the current Lua coroutine and returns control back to the worker goroutine's main execution loop.  
4. The worker goroutine, now free from the Lua script's execution, can wait for the Go I/O operation to complete (e.g., by reading from a channel). It is free to do other work in the meantime, though in this model it will typically just wait for the I/O of the coroutine it is managing.  
5. Once the Go I/O operation completes, the worker goroutine calls lua\_resume(), passing the result (e.g., the data read from the socket) back into the Lua coroutine.  
6. The Lua script resumes execution seamlessly from the exact point it yielded, as if the call had been synchronous, but without ever having blocked the worker goroutine.

This yield/resume cycle is the fundamental mechanism that allows a single-threaded worker goroutine to effectively juggle thousands of concurrent I/O-bound requests.

## **4\. Data Scoping and State Management**

The multi-layered architecture defines three distinct scopes for data, which is essential for writing correct and performant applications.

### **4.1. Per-Request State: golapis.ctx**

To pass data between different phases of a single request (e.g., from an auth script to a content generation script), the system will provide a golapis.ctx table.

* **Implementation:** When a worker goroutine creates a Lua coroutine for a new request, it will also create a new, empty Lua table. This table will be made available to the script (e.g., as a global golapis.ctx).  
* **Lifecycle:** The golapis.ctx table's lifetime is strictly bound to that of the request. It is created when the request's coroutine is spawned and becomes eligible for garbage collection after the request is completed, ensuring complete isolation.

### **4.2. Per-Worker State: Module-Level Variables**

Data that needs to be shared among all requests handled by a single worker goroutine can be stored in module-level variables in Lua.

* **Implementation:** Due to the per-worker Lua VM and code caching, a Lua module's top-level code is executed only once when it is first required by a worker. Any variables declared at the module level will persist for the lifetime of that worker.  
* **Use Cases:** This scope is ideal for read-only data or resources that are expensive to create, such as compiled regular expressions, loaded configuration, or, most importantly, **connection pools** to upstream services (e.g., databases, caches).12 Each worker would maintain its own pool of connections.  
* **Concurrency Warning:** Because all coroutines within a worker can access these variables, they must be treated as read-only after initialization. If mutable state is required, access must be protected by a locking mechanism, as a yield between a read and a write can create a race condition.

### **4.3. Global, Cross-Worker State: Shared Dictionary**

To share state across all worker goroutines within the single Go process, a global, thread-safe key-value store will be provided. This is a simpler and more performant alternative to Nginx's lua\_shared\_dict, which relies on OS-level shared memory.13

* **Implementation:** A Go sync.Map or a standard map\[string\]interface{} protected by a sync.RWMutex will be instantiated at the application level.  
* **API Exposure:** A set of Go functions will be exposed to the Lua environment to interact with this map (e.g., golapis.shared.get(key), golapis.shared.set(key, value), golapis.shared.incr(key, n)). These functions will handle the necessary locking internally to ensure safe concurrent access from all worker goroutines.

## **5\. Risks and Mitigation**

* **cgo Performance Overhead:** While cgo calls have a non-trivial fixed overhead, this design mitigates the impact by adopting the "fat interface" principle. The architecture is intended for scenarios where a significant amount of logic is executed within Lua for each boundary crossing, thus amortizing the call cost.15  
* **Blocking Calls:** The single greatest risk to performance is the accidental use of a standard blocking function from within a Lua script (e.g., io.popen, os.execute). This would freeze the entire worker goroutine, stalling all other requests it is managing. **Mitigation:** Strict developer discipline and providing a comprehensive suite of non-blocking go.\* APIs are essential. Static analysis tools could be developed to detect the use of forbidden blocking functions in Lua scripts.  
* **Build & Deployment Complexity:** The use of cgo means the project will no longer be a pure Go binary. It will require a C compiler (like gcc) and the LuaJIT library to be present on the build machine. This sacrifices Go's hallmark of simple cross-compilation.  
  **Mitigation:** Use of Docker containers for building and deployment can standardize the environment and encapsulate these dependencies.  
* **Data Marshalling:** Passing complex data structures (structs, slices) between Go and Lua requires copying. This can become a bottleneck if large, complex payloads are frequently transferred across the boundary.  
  **Mitigation:** For complex data, it is more efficient to serialize it into a flat format (like JSON or Protobuf) in one environment, pass the resulting single byte slice, and deserialize it in the other.  
* **Lua Coroutine Stability:** The chosen Go-Lua binding library may have limitations regarding coroutine support. The golua library, for example, notes that its coroutine implementation is largely untested.  
  **Mitigation:** This area requires extensive and rigorous testing to ensure the yield/resume mechanism is robust and handles all edge cases, including errors and panics, correctly.

