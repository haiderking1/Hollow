// PORT: backend/agent/memory_correction.go

// userMessageSignalsProfileCorrection reports whether the user's message likely
// corrects how the assistant addressed them or interpreted USER PROFILE text.
// When true, we force an immediate background memory review so misread profile
// entries get replaced even if the foreground model only apologizes.
export function userMessageSignalsProfileCorrection(msg: string): boolean {
  const m = msg.trim().toLowerCase();
  if (m === "") {
    return false;
  }
  const signals = [
    "my name is",
    "my name's",
    "not my name",
    "that's not my name",
    "that is not my name",
    "call me ",
    "don't call me",
    "do not call me",
    "stop calling me",
    "wrong name",
    "not just h",
    "not the letter",
    "way too literally",
    "misread",
    "you got my name",
    "you have my name wrong",
    "wym hey",
    "bro my name",
    "lowercase h not",
    "name is haider",
  ];
  for (const s of signals) {
    if (m.includes(s)) {
      return true;
    }
  }
  return false;
}

/*
PORT STATUS
source path: backend/agent/memory_correction.go
source lines: 43
draft lines: 37
confidence: high
status: phase_b_compile
*/
