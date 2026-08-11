## MODIFIED Requirements

### Requirement: One automatic submission per target connection
JumpOTP SHALL automatically submit at most one provider-generated code per distinct strict-matched MFA prompt, and at most two automatic submissions per connection attempt. A second automatic submission SHALL occur only after the remote endpoint rejects the first and SHALL use a code obtained after waiting for the next epoch-aligned TOTP window boundary; a previously submitted code value MUST NOT be resubmitted. After the second rejection, or when no fresh code can be obtained, JumpOTP MUST NOT loop and SHALL follow the configured fallback. Each supervised reconnection attempt is a new connection attempt with reset submission state.

#### Scenario: Automatic code rejected
- **WHEN** the remote endpoint rejects an automatically submitted code and shows another strict-matched prompt
- **THEN** JumpOTP waits for the next TOTP window, submits one fresh code automatically, and preserves all redaction and zeroization guarantees

#### Scenario: Automatic submissions exhausted
- **WHEN** the remote endpoint rejects a second automatically submitted code and shows another prompt
- **THEN** JumpOTP leaves the target interactive, explains that automatic submission is exhausted, and allows only manual interaction for that connection attempt

#### Scenario: Stale code is never repeated
- **WHEN** a retry becomes possible while the rejected code's TOTP window is still current
- **THEN** JumpOTP delays the retry past the window boundary rather than resubmitting the same value

### Requirement: Ephemeral grouped workspace broker
Each active `workspace PROFILE` invocation SHALL create one current-user-only Unix-domain broker socket in a mode-`0700` runtime directory and remove it when the invocation ends. Target wrappers SHALL register only their profile and target identifiers; the broker SHALL resolve effective provider and item values from its own validated configuration. Targets with the same provider and item SHALL form one group. The broker SHALL retrieve only after at least one group member is ready, briefly aggregate concurrently ready members, and send one in-memory code to each ready wrapper over the socket. Later members SHALL trigger a fresh retrieval. When a group retrieval would begin with fewer than 8 seconds remaining in the current epoch-aligned 30-second TOTP window, the broker SHALL delay the retrieval until the next window begins so every delivered code retains usable submission runway.

#### Scenario: Two targets ready together
- **WHEN** two targets in the same group reach their prompts within the aggregation window while the broker is active
- **THEN** the broker calls Bitwarden once and sends the result once to each ready target wrapper

#### Scenario: Late target
- **WHEN** another target in the group reaches its prompt after the earlier batch was submitted
- **THEN** the broker performs a fresh provider retrieval for the late target

#### Scenario: Retrieval near the rotation boundary
- **WHEN** a group becomes ready inside the final guard interval of the current TOTP window
- **THEN** the broker waits for the next window boundary before invoking the provider and then delivers the fresh-window code to every aggregated waiter

#### Scenario: Target-level OTP override
- **WHEN** one target overrides the profile Bitwarden item
- **THEN** its code is never delivered to targets using the profile item

#### Scenario: Broker disappears
- **WHEN** the workspace command detaches, terminates, or loses its broker while target panes remain
- **THEN** target sessions persist, pending wrappers reconnect to a later broker invocation, and a wrapper with an attended prompt can still use visible manual fallback
