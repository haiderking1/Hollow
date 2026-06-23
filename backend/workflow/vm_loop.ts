// PORT: backend/workflow/vm_loop.go

export class vmLoop {
  private activePromisesCount = 0;

  async(ctx: AbortSignal, work: () => Promise<any>): Promise<any> {
    this.activePromisesCount++;
    return new Promise((resolve, reject) => {
      let settled = false;
      const onAbort = () => {
        if (settled) return;
        settled = true;
        this.activePromisesCount--;
        reject(new Error("interrupted"));
      };
      if (ctx.aborted) {
        onAbort();
        return;
      }
      ctx.addEventListener("abort", onAbort);

      work()
        .then((val) => {
          if (settled) return;
          settled = true;
          ctx.removeEventListener("abort", onAbort);
          this.activePromisesCount--;
          resolve(val);
        })
        .catch((err) => {
          if (settled) return;
          settled = true;
          ctx.removeEventListener("abort", onAbort);
          this.activePromisesCount--;
          reject(err);
        });
    });
  }

  wait(): void {
    // wait handles standard Node/Bun thread settle which is native.
  }

  async await(ctx: AbortSignal, value: any): Promise<any> {
    if (value === undefined || value === null) {
      return undefined;
    }
    if (typeof value.then === "function") {
      if (ctx.aborted) {
        throw new Error("interrupted");
      }
      return new Promise((resolve, reject) => {
        let settled = false;
        const onAbort = () => {
          if (settled) return;
          settled = true;
          reject(new Error("interrupted"));
        };
        ctx.addEventListener("abort", onAbort);
        value
          .then((res: any) => {
            if (settled) return;
            settled = true;
            ctx.removeEventListener("abort", onAbort);
            resolve(res);
          })
          .catch((err: any) => {
            if (settled) return;
            settled = true;
            ctx.removeEventListener("abort", onAbort);
            reject(err);
          });
      });
    }
    return value;
  }
}

export function newVMLoop(): vmLoop {
  return new vmLoop();
}

/*
PORT STATUS
source path: backend/workflow/vm_loop.go
source lines: 75
draft lines: 55
confidence: high
status: phase_b_compile
*/
