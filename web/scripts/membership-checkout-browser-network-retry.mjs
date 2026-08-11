const NETWORK_CHANGED_PREFIX = "net::ERR_NETWORK_CHANGED ";

export class MembershipCheckoutBrowserCaseError extends Error {
    constructor(message, failures, options) {
        super(message, options);
        this.name = "MembershipCheckoutBrowserCaseError";
        this.failedResponses = [...failures.failedResponses];
        this.requestFailures = [...failures.requestFailures];
    }
}

export function shouldRetryInitialNetworkChange({ attempt, completedCases, failedResponses, requestFailures }) {
    return attempt === 0 && completedCases === 0 && failedResponses.length === 0 && requestFailures.length > 0 && requestFailures.every((failure) => failure.startsWith(NETWORK_CHANGED_PREFIX));
}
