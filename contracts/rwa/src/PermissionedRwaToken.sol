// SPDX-License-Identifier: MIT
pragma solidity 0.8.30;

/// @notice Local simulation only. This token is not represented as a legally issued security.
/// @dev One zero-decimal token represents one locked, settled whole share in the simulator.
contract PermissionedRwaToken {
    error AlreadyProcessed(bytes32 operationId);
    error Frozen(address account);
    error InsufficientAllowance();
    error InsufficientBalance();
    error InvalidAmount();
    error InvalidChain(uint256 chainId);
    error InvalidParty(address account);
    error NotAdmin();
    error NotPermissioned(address account);
    error Paused();

    string public name;
    string public symbol;
    uint8 public constant decimals = 0;
    uint256 public constant BASE_SEPOLIA_CHAIN_ID = 84532;

    address public immutable admin;
    address public immutable platformVault;
    uint256 public totalSupply;
    bool public paused;

    mapping(address => uint256) public balanceOf;
    mapping(address => mapping(address => uint256)) public allowance;
    mapping(address => bool) public permissioned;
    mapping(address => bool) public frozen;
    mapping(bytes32 => uint8) public operationKind;

    event Approval(address indexed owner, address indexed spender, uint256 amount);
    event Transfer(address indexed from, address indexed to, uint256 amount);
    event PermissionUpdated(address indexed account, bool permissioned);
    event FrozenUpdated(address indexed account, bool frozen);
    event PauseUpdated(bool paused);
    event OperationExecuted(bytes32 indexed operationId, uint8 indexed kind, address indexed account, uint256 amount);
    event ForcedRedemption(bytes32 indexed operationId, address indexed account, uint256 amount);

    constructor(string memory tokenName, string memory tokenSymbol, address tokenAdmin, address vault) {
        if (block.chainid != BASE_SEPOLIA_CHAIN_ID) revert InvalidChain(block.chainid);
        if (tokenAdmin == address(0) || vault == address(0)) revert InvalidParty(address(0));
        name = tokenName;
        symbol = tokenSymbol;
        admin = tokenAdmin;
        platformVault = vault;
        permissioned[tokenAdmin] = true;
        permissioned[vault] = true;
        emit PermissionUpdated(tokenAdmin, true);
        emit PermissionUpdated(vault, true);
    }

    modifier onlyAdmin() {
        if (msg.sender != admin) revert NotAdmin();
        _;
    }

    modifier whenNotPaused() {
        if (paused) revert Paused();
        _;
    }

    function setPermission(address account, bool allowed) external onlyAdmin {
        if (account == address(0)) revert InvalidParty(account);
        permissioned[account] = allowed;
        emit PermissionUpdated(account, allowed);
    }

    function setFrozen(address account, bool value) external onlyAdmin {
        if (account == address(0)) revert InvalidParty(account);
        frozen[account] = value;
        emit FrozenUpdated(account, value);
    }

    function setPaused(bool value) external onlyAdmin {
        paused = value;
        emit PauseUpdated(value);
    }

    function approve(address spender, uint256 amount) external whenNotPaused returns (bool) {
        _requireTransferParty(msg.sender);
        _requireTransferParty(spender);
        allowance[msg.sender][spender] = amount;
        emit Approval(msg.sender, spender, amount);
        return true;
    }

    function transfer(address to, uint256 amount) external whenNotPaused returns (bool) {
        _transfer(msg.sender, to, amount);
        return true;
    }

    function transferFrom(address from, address to, uint256 amount) external whenNotPaused returns (bool) {
        uint256 authorized = allowance[from][msg.sender];
        if (authorized < amount) revert InsufficientAllowance();
        allowance[from][msg.sender] = authorized - amount;
        _transfer(from, to, amount);
        return true;
    }

    function mint(bytes32 operationId, address to, uint256 amount) external onlyAdmin whenNotPaused {
        _beginOperation(operationId, 1);
        _requireTransferParty(to);
        if (amount == 0) revert InvalidAmount();
        totalSupply += amount;
        balanceOf[to] += amount;
        emit Transfer(address(0), to, amount);
        emit OperationExecuted(operationId, 1, to, amount);
    }

    function burn(bytes32 operationId, address from, uint256 amount) external onlyAdmin whenNotPaused {
        _beginOperation(operationId, 2);
        _burn(from, amount);
        emit OperationExecuted(operationId, 2, from, amount);
    }

    /// @notice Admin recovery path. It remains available while globally paused.
    function forcedRedemption(bytes32 operationId, address from, uint256 amount) external onlyAdmin {
        _beginOperation(operationId, 3);
        _burn(from, amount);
        emit OperationExecuted(operationId, 3, from, amount);
        emit ForcedRedemption(operationId, from, amount);
    }

    function adminTransfer(bytes32 operationId, address from, address to, uint256 amount)
        external
        onlyAdmin
        whenNotPaused
    {
        _beginOperation(operationId, 4);
        _transfer(from, to, amount);
        emit OperationExecuted(operationId, 4, to, amount);
    }

    function operationExecuted(bytes32 operationId) external view returns (bool) {
        return operationKind[operationId] != 0;
    }

    function _beginOperation(bytes32 operationId, uint8 kind) internal {
        if (operationId == bytes32(0)) revert InvalidAmount();
        if (operationKind[operationId] != 0) revert AlreadyProcessed(operationId);
        operationKind[operationId] = kind;
    }

    function _burn(address from, uint256 amount) internal {
        if (from == address(0) || amount == 0) revert InvalidAmount();
        uint256 balance = balanceOf[from];
        if (balance < amount) revert InsufficientBalance();
        balanceOf[from] = balance - amount;
        totalSupply -= amount;
        emit Transfer(from, address(0), amount);
    }

    function _transfer(address from, address to, uint256 amount) internal {
        _requireTransferParty(from);
        _requireTransferParty(to);
        if (amount == 0) revert InvalidAmount();
        uint256 balance = balanceOf[from];
        if (balance < amount) revert InsufficientBalance();
        balanceOf[from] = balance - amount;
        balanceOf[to] += amount;
        emit Transfer(from, to, amount);
    }

    function _requireTransferParty(address account) internal view {
        if (account == address(0)) revert InvalidParty(account);
        if (!permissioned[account]) revert NotPermissioned(account);
        if (frozen[account]) revert Frozen(account);
    }
}
